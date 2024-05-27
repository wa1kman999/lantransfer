package msg

import (
	"bytes"
	"encoding/binary"
)

const headerLen = 8

type Packager struct{}

func NewPackager() *Packager {
	return &Packager{}
}

func (p *Packager) GetHeadLen() uint32 {
	return headerLen
}

func (p *Packager) Pack(msg *Message) ([]byte, error) {
	buff := bytes.NewBuffer([]byte{})
	if err := binary.Write(buff, binary.BigEndian, msg.GetMsgID()); err != nil {
		return nil, err
	}
	if err := binary.Write(buff, binary.BigEndian, msg.GetDataLen()); err != nil {
		return nil, err
	}
	if err := binary.Write(buff, binary.BigEndian, msg.GetData()); err != nil {
		return nil, err
	}
	return buff.Bytes(), nil
}

func (p *Packager) Unpack(data []byte) (*Message, error) {
	buff := bytes.NewReader(data)
	msg := &Message{}
	if err := binary.Read(buff, binary.BigEndian, &msg.ID); err != nil {
		return nil, err
	}
	if err := binary.Read(buff, binary.BigEndian, &msg.DataLen); err != nil {
		return nil, err
	}
	return msg, nil
}
