package core_func

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/OpenIMSDK/tools/log"
	"github.com/openimsdk/openim-sdk-core/v3/internal/login"
	"github.com/openimsdk/openim-sdk-core/v3/pkg/ccontext"
	"github.com/openimsdk/openim-sdk-core/v3/pkg/sdkerrs"
	"github.com/openimsdk/openim-sdk-core/v3/pkg/utils"
)

const (
	Success = "OnSuccess"
	Failed  = "OnError"
)

type EventData struct {
	Event       string `json:"event"`
	ErrCode     int32  `json:"errCode"`
	ErrMsg      string `json:"errMsg"`
	Data        string `json:"data"`
	OperationID string `json:"operationID"`
}

type FuncRouter struct {
	userForSDK  *login.LoginMgr
	respMessage *RespMessage
}

func NewFuncRouter(respMessagesChan chan *EventData) *FuncRouter {
	return &FuncRouter{respMessage: NewRespMessage(respMessagesChan)}
}

// 调用函数，并处理函数返回结果
func (f *FuncRouter) call(operationID string, fn any, args ...any) {
	go func() {
		res, err := f.call_(operationID, fn, args...)
		if err != nil {
			f.respMessage.sendOnErrorResp(operationID, err)
		}
		data, err := json.Marshal(res)
		if err != nil {
			f.respMessage.sendOnErrorResp(operationID, err)
			return
		} else {
			f.respMessage.sendOnSuccessResp(operationID, string(data))
		}
	}()
}

// CheckResourceLoad checks the SDK is resource load status.
func CheckResourceLoad(uSDK *login.LoginMgr, funcName string) error {
	if uSDK == nil {
		return utils.Wrap(errors.New("CheckResourceLoad failed uSDK == nil "), "")
	}
	if funcName == "" {
		return nil
	}
	parts := strings.Split(funcName, ".")
	if parts[len(parts)-1] == "Login-fm" {
		return nil
	}
	// 判断 Friend、User、Group、Conversation、Full 是否为 null
	if uSDK.Friend() == nil || uSDK.User() == nil || uSDK.Group() == nil || uSDK.Conversation() == nil ||
		uSDK.Full() == nil {
		return utils.Wrap(errors.New("CheckResourceLoad failed, resource nil "), "")
	}
	return nil
}

// 具体的远程调用逻辑
func (f *FuncRouter) call_(operationID string, fn any, args ...any) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("panic: %+v\n%s", r, debug.Stack())
			err = fmt.Errorf("call panic: %+v", r)
		}
	}()
	// 反射获取 fn 类型的值及类型
	funcPtr := reflect.ValueOf(fn).Pointer()
	funcName := runtime.FuncForPC(funcPtr).Name()

	if operationID == "" {
		return nil, sdkerrs.ErrArgs.Wrap("call function operationID is empty")
	}
	// 检测 skd 资源是否就绪
	if err := CheckResourceLoad(f.userForSDK, funcName); err != nil {
		return nil, sdkerrs.ErrResourceLoad.Wrap("not load resource")
	}

	// 传入 operationID，获取 ctx
	ctx := ccontext.WithOperationID(f.userForSDK.BaseCtx(), operationID)
	log.ZInfo(ctx, "call function", "in sdk args", args)

	fnv := reflect.ValueOf(fn)
	if fnv.Kind() != reflect.Func {
		return nil, sdkerrs.ErrSdkInternal.Wrap(fmt.Sprintf("call function fn is not function, is %T", fn))
	}
	fnt := fnv.Type()
	nin := fnt.NumIn()
	if len(args)+1 != nin {
		return nil, sdkerrs.ErrSdkInternal.Wrap(fmt.Sprintf("go code error: fn in args num is not match"))
	}
	t := time.Now()
	log.ZInfo(ctx, "input req", "function name", funcName, "args", args)
	ins := make([]reflect.Value, 0, nin)
	ins = append(ins, reflect.ValueOf(ctx))
	for i := 0; i < len(args); i++ {
		inFnField := fnt.In(i + 1)
		arg := reflect.TypeOf(args[i])
		if arg.String() == inFnField.String() || inFnField.Kind() == reflect.Interface {
			ins = append(ins, reflect.ValueOf(args[i]))
			continue
		}
		if arg.Kind() == reflect.String { // json
			var ptr int
			for inFnField.Kind() == reflect.Ptr {
				inFnField = inFnField.Elem()
				ptr++
			}
			switch inFnField.Kind() {
			case reflect.Struct, reflect.Slice, reflect.Array, reflect.Map:
				v := reflect.New(inFnField)
				if err := json.Unmarshal([]byte(args[i].(string)), v.Interface()); err != nil {
					return nil, sdkerrs.ErrSdkInternal.Wrap(fmt.Sprintf("go call json.Unmarshal error: %s",
						err))
				}
				if ptr == 0 {
					v = v.Elem()
				} else if ptr != 1 {
					for i := ptr - 1; i > 0; i-- {
						temp := reflect.New(v.Type())
						temp.Elem().Set(v)
						v = temp
					}
				}
				ins = append(ins, v)
				continue
			}
		}
		return nil, sdkerrs.ErrSdkInternal.Wrap(fmt.Sprintf("go code error: fn in args type is not match"))
	}
	outs := fnv.Call(ins)
	if len(outs) == 0 {
		return "", nil
	}
	if fnt.Out(len(outs) - 1).Implements(reflect.ValueOf(new(error)).Elem().Type()) {
		if errValueOf := outs[len(outs)-1]; !errValueOf.IsNil() {
			log.ZError(ctx, "fn call error", errValueOf.Interface().(error), "function name",
				funcName, "cost time", time.Since(t))
			return nil, errValueOf.Interface().(error)
		}
		if len(outs) == 1 {
			return "", nil
		}
		outs = outs[:len(outs)-1]
	}
	for i := 0; i < len(outs); i++ {
		out := outs[i]
		switch out.Kind() {
		case reflect.Map:
			if out.IsNil() {
				outs[i] = reflect.MakeMap(out.Type())
			}
		case reflect.Slice:
			if out.IsNil() {
				outs[i] = reflect.MakeSlice(out.Type(), 0, 0)
			}
		}
	}
	if len(outs) == 1 {
		log.ZInfo(ctx, "output resp", "function name", funcName, "resp", outs[0].Interface(),
			"cost time", time.Since(t))
		return outs[0].Interface(), nil
	}
	val := make([]any, 0, len(outs))
	for i := range outs {
		val = append(val, outs[i].Interface())
	}
	log.ZInfo(ctx, "output resp", "function name", funcName, "resp", val, "cost time", time.Since(t))
	return val, nil
}