package sender

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wa1kman999/lantransfer/copier"
	"github.com/wa1kman999/lantransfer/global"
	"github.com/wa1kman999/lantransfer/msg"

	"github.com/schollz/progressbar/v3"
)

type Sender struct {
	ctx       context.Context
	cancel    context.CancelFunc
	packer    *msg.Packager
	udpConn   *net.UDPConn
	tranConn  net.Conn
	fileInfo  *fileInfo
	abortChan chan os.Signal
	ticker    *time.Ticker
}

type fileInfo struct {
	name      string
	size      int64
	calMd5    hash.Hash
	isSending bool
}

func NewSender() *Sender {
	srcAddr := &net.UDPAddr{IP: net.IPv4zero}
	conn, err := net.ListenUDP("udp", srcAddr)
	if err != nil {
		log.Fatalln("Discoverer listenUDP error:", err)
	}

	s := &Sender{
		udpConn: conn,
		packer:  msg.NewPackager(),
		ticker:  time.NewTicker(time.Second * 3),
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.abortChan = make(chan os.Signal, 1)
	signal.Notify(s.abortChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go s.abort()
	return s
}

func (d *Sender) abort() {
	for range d.abortChan {
		d.cancel()
	}
}

func (d *Sender) getBroadcastAddr() []net.IP {
	interfaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}
	var broadcastAddress []net.IP
	for _, face := range interfaces {
		if (face.Flags & (net.FlagUp | net.FlagBroadcast | net.FlagLoopback)) == (net.FlagBroadcast | net.FlagUp) {
			ips, err := face.Addrs()
			if err != nil {
				panic(err)
			}
			for _, addr := range ips {
				if ipNet, ok := addr.(*net.IPNet); ok {
					if ipNet.IP.To4() != nil {
						var fields net.IP
						for i := 0; i < 4; i++ {
							fields = append(fields, (ipNet.IP.To4())[i]|^ipNet.Mask[i])
						}
						broadcastAddress = append(broadcastAddress, fields)
					}
				}
			}
		}
	}
	return broadcastAddress
}

func (d *Sender) checkFile(fileName string) error {
	fi, err := os.Stat(fileName)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return errors.New("file is dir")
	}
	if fi.Size() == 0 {
		return errors.New("file size is 0")
	}
	d.fileInfo = &fileInfo{
		name:   fi.Name(),
		size:   fi.Size(),
		calMd5: md5.New(),
	}
	return nil
}

func (d *Sender) Send(fileName string) {
	if err := d.checkFile(fileName); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("file sending...")
	ips := d.getBroadcastAddr()
	go d.broadcast(ips)
	if err := d.recvfromUdp(); err == nil {
		fmt.Printf("SEND %s SUCCESS!!!\n", fileName)
	}
}

func (d *Sender) broadcast(ips []net.IP) {
	fileName := []byte(d.fileInfo.name)
	data := make([]byte, len(fileName)+4)
	binary.BigEndian.PutUint32(data, msg.UdpReq)
	copy(data[4:], fileName)
	for range d.ticker.C {
		if d.fileInfo.isSending {
			d.ticker.Stop()
			return
		}
		for _, ip := range ips {
			_, err := d.udpConn.WriteToUDP(data, &net.UDPAddr{IP: ip, Port: global.ReceiverPort})
			if err != nil {
				fmt.Println("broadcast err:", err)
			}
		}
	}
}

func (d *Sender) recvfromUdp() error {
	for {
		data := make([]byte, 1024)
		n, remoteAddr, err := d.udpConn.ReadFromUDP(data)
		if err != nil {
			return err
		}
		if binary.BigEndian.Uint32(data[:4]) == msg.UdpAcK {
			port := binary.BigEndian.Uint32(data[4:n])
			d.fileInfo.isSending = true
			return d.dialT(&net.TCPAddr{IP: remoteAddr.IP, Port: int(port)})
		}
	}
}

func (d *Sender) dialT(remoteAddr *net.TCPAddr) error {
	conn, err := net.Dial("tcp", remoteAddr.String())
	if err != nil {
		return err
	}
	d.tranConn = conn
	defer conn.Close()
	err = d.sendFile(conn)
	return err
}

func (d *Sender) recvFromTcp() error {
	headData := make([]byte, d.packer.GetHeadLen())
	// _, err := io.ReadFull(d.tranConn, headData)
	// if err != nil {
	// 	return err
	// }
	io.ReadFull(d.tranConn, headData)
	message, err := d.packer.Unpack(headData)
	if err != nil {
		return err
	}
	if message.ID == msg.TcpAbort {
		return errors.New("receiver abort")
	}
	return nil
}

func (d *Sender) sendFile(conn net.Conn) error {
	go func() {
		if err := d.recvFromTcp(); err != nil {
			fmt.Printf("\nSending error: %s!\n", err)
			d.abortChan <- os.Kill
		}
	}()
	f, err := os.Open(d.fileInfo.name)
	if err != nil {
		return err
	}
	defer f.Close()

	fileSize := make([]byte, 8)
	binary.BigEndian.PutUint64(fileSize, uint64(d.fileInfo.size))
	if err := d.sendData(msg.TcpFileSize, fileSize); err != nil {
		return err
	}
	if err := d.sendSign(msg.TcpFile); err != nil {
		return err
	}
	bar := progressbar.DefaultBytes(
		d.fileInfo.size,
		fmt.Sprintf("%s Sending", d.fileInfo.name),
	)

	_, err = copier.CopyN(d.ctx, io.MultiWriter(conn, bar, d.fileInfo.calMd5), f, d.fileInfo.size)
	if err != nil {
		return err
	}
	if err := d.sendData(msg.TcpFileMd5, d.fileInfo.calMd5.Sum(nil)); err != nil {
		return err
	}
	return d.sendSign(msg.TcpOver)
}

func (d *Sender) sendSign(signal msg.Signal) error {
	sign := make([]byte, 8)
	binary.BigEndian.PutUint32(sign, uint32(signal))
	_, err := d.tranConn.Write(sign)
	return err
}

func (d *Sender) sendData(signal msg.Signal, data []byte) error {
	m := msg.NewMsgPackage(signal, data)
	pack, err := d.packer.Pack(m)
	if err != nil {
		return err
	}
	_, err = d.tranConn.Write(pack)
	return err
}
