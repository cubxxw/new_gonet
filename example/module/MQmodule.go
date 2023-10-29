package module

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/xuexihuang/new_gonet/common"
	"github.com/xuexihuang/new_gonet/gate"
	log "github.com/xuexihuang/new_log15"
)

const (
	EVENT_SUB_CHAN_LENGTH = 50
	EVENT_STREAM_NAME     = "aistudio-web-push"
	EVENT_SUB_TIMEOUT     = 5 //5 Millisecond
)

type ParamStru struct {
	Token     string
	SessionId string
	UserId    int64
	GroupId   int64
	OrgId     int64
	OrgName   string
}

type MQPushActorIm struct {
	heartTickerSend *time.Ticker //用于心跳send
	param           *ParamStru
	nChanLen        int //接收数据网络缓存
	wg              sync.WaitGroup
	a               gate.Agent
	SessionId       string
	closeChan       chan bool        //主动关闭协程的通道
	ReceivMsgChan   chan interface{} //接收网络层数据通道
	heartTicker     *time.Ticker     //用于心跳监测
	heartFlag       bool             //初始为false，收到心跳pack设置为true
	isclosing       bool
}

func NewMQActor(a gate.Agent, sessionId string, appParam *ParamStru) (MQPushActor, error) {
	ret := &MQPushActorIm{param: appParam, a: a, SessionId: sessionId, closeChan: make(chan bool, 1), nChanLen: 10, ReceivMsgChan: make(chan interface{}, 10), isclosing: false,
		heartTicker: time.NewTicker(15 * time.Second), heartFlag: false, heartTickerSend: time.NewTicker(5 * time.Second)}
	///////////////////////////////////////
	res := &ResponseSt{Type: HEART_CONFIG_TYPE, Success: true, Rate: 5}
	ret.sendResp(res)
	///////////////////////////////////////
	go ret.run()
	return ret, nil
}

func (actor *MQPushActorIm) run() {
	actor.wg.Add(1)
	defer common.TryRecoverAndDebugPrint()
	defer actor.wg.Done()
	for {
		select {
		case <-actor.heartTickerSend.C: //send the heart pack
			if actor.isclosing == true {
				continue
			}
			actor.sendResp(&ResponseSt{Type: "heart"})
		case <-actor.closeChan:
			log.Info("收到退出信号", "sessionId", actor.SessionId)
			return
		case recvData := <-actor.ReceivMsgChan:
			if actor.isclosing == true {
				continue
			}
			data := recvData.(*common.TWSData)
			_ = actor.doRecvPro(data)
		case <-actor.heartTicker.C:
			if actor.heartFlag == true {
				actor.heartFlag = false
			} else {
				log.Error("心跳包超时错误", "sessionId", actor.SessionId)
				actor.isclosing = true
				actor.a.Destroy()
			}
		}
	}
}
func (actor *MQPushActorIm) Destroy() {
	actor.closeChan <- true
	actor.wg.Wait()
	actor.a = nil
	log.Info("退出MQPushActorIm", "sessionId", actor.SessionId)
}
func (actor *MQPushActorIm) ProcessRecvMsg(msg interface{}) error {
	if len(actor.ReceivMsgChan) == actor.nChanLen {
		log.Error("send channel is full", "sessionId", actor.SessionId)
		return errors.New("send channel is full")
	}
	actor.ReceivMsgChan <- msg
	return nil
}

func (actor *MQPushActorIm) doRecvPro(data *common.TWSData) error {
	if data.MsgType == common.TextMsg {
		req := &RequestSt{}
		err := json.Unmarshal(data.Msg, req)
		if err != nil {
			log.Error("解析前端协议出错", "err", err, "sessionId", actor.SessionId)
			return err
		}
		if req.Cmd == HEART_CMD {
			actor.heartFlag = true
		} else if req.Cmd == SUB_CMD && req.Topic != "" {
			log.Info("收到sub命令", "req", req, "sessionId", actor.SessionId)
			res := &ResponseSt{Type: RESP_OP_TYPE, Cmd: SUB_CMD, Topic: req.Topic, RequestId: req.RequestId, Success: true,
				MsgSeqId: req.MsgSeqId, MsgTimeStamp: req.MsgTimeStamp}
			actor.sendResp(res)
		} else {
			log.Error("前端协议cmd字段不是自定义字符串", "req", string(data.Msg), "sessionId", actor.SessionId)
			return errors.New("前端协议cmd字段不是自定义字符串")
		}
	}
	return nil
}

func (actor *MQPushActorIm) sendResp(res *ResponseSt) {
	resb, _ := json.Marshal(res)
	resSend := &common.TWSData{MsgType: common.TextMsg, Msg: resb}
	actor.a.WriteMsg(resSend)
}