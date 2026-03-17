package datagram

import (
	"encoding/binary"
)

const (
	CommandConnect Cmd = iota + 1
	CommandForward
	CommandClose
	CommandRetry
)

type PayloadConnect struct {
	Host string
	Port uint16
}

func (pld *PayloadConnect) Marshal() []byte {
	data := []byte(pld.Host)
	data = binary.BigEndian.AppendUint16(data, pld.Port)

	return data
}

func (pld *PayloadConnect) Unmarshal(data []byte) error {
	if len(data) < 2 {
		return ErrMalformed
	}

	pld.Host = string(data[:len(data)-2])
	pld.Port = binary.BigEndian.Uint16(data[len(data)-2:])

	return nil
}

type PayloadRetry struct {
	Number Num
}

func (pld *PayloadRetry) Marshal() []byte {
	data := make([]byte, 4)

	binary.BigEndian.PutUint32(data, uint32(pld.Number))

	return data
}

func (pld *PayloadRetry) Unmarshal(data []byte) error {
	if len(data) < 4 {
		return ErrMalformed
	}

	pld.Number = Num(binary.BigEndian.Uint32(data))

	return nil
}
