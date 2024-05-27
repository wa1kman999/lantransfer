package msg

type Signal uint32

const (
	TcpFileSize Signal = 0xf1
	TcpFileMd5  Signal = 0xf2
	TcpFile     Signal = 0xf3
	TcpOver     Signal = 0xff
	TcpAbort    Signal = 0xfe

	UdpReq = 0xe1
	UdpAcK = 0xe2
)

type Message struct {
	ID      Signal
	DataLen uint32
	Data    []byte
}

func NewMsgPackage(ID Signal, data []byte) *Message {
	return &Message{
		ID:      ID,
		DataLen: uint32(len(data)),
		Data:    data,
	}
}

func NewSignalPackage(ID Signal) *Message {
	return &Message{
		ID:      ID,
		DataLen: 0,
		Data:    nil,
	}
}

func (msg *Message) GetDataLen() uint32 {
	return msg.DataLen
}

func (msg *Message) GetMsgID() Signal {
	return msg.ID
}
func (msg *Message) GetData() []byte {
	return msg.Data
}
