package module

const (
	RESP_OP_TYPE = "response"
	HEART_CMD    = "heart"
)

type ResponseSt struct {
	Type     string   `json:"type"`    // "response" or "heart"
	Success  bool     `json:"success"` //
	ErrMsg   string   `json:"errMsg"`
	UserId   []string `json:"userIds"`
	Duration int64    `json:"duration"` // progress run time ,seconds
}

type RequestSt struct {
	Type         string `json:"type"` //
	RequestId    string `json:"requestId"`
	Topic        string `json:"topic"`
	Extra        string `json:"extra"`
	MsgTimeStamp int64  `json:"msgStartTime"`
	MsgSeqId     int64  `json:"msgSeqId"`
}

///////////////////////////////////////////////////////////////////////
