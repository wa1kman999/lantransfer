package receiver

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"hash"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/wa1kman999/lantransfer/copier"
	"github.com/wa1kman999/lantransfer/global"
	"github.com/wa1kman999/lantransfer/msg"

	"github.com/schollz/progressbar/v3"
)

type info struct {
	localAddr  *net.UDPAddr
	remoteAddr *net.UDPAddr
	fileName   string
}

type fileInfo struct {
	name   string
	size   int64
	md5    []byte
	calMd5 hash.Hash
}

type Receiver struct {
	ctx        context.Context
	cancel     context.CancelFunc
	udpConn    *net.UDPConn
	noticeChan chan *info
	receiving  bool
	packer     *msg.Packager
	fileInfo   *fileInfo
	abortChan  chan os.Signal
	tranConn   net.Conn
}

func NewReceiver() *Receiver {
	r := &Receiver{
		noticeChan: make(chan *info, 1),
		packer:     msg.NewPackager(),
		fileInfo: &fileInfo{
			calMd5: md5.New(),
		},
	}
	r.ctx, r.cancel = context.WithCancel(context.Background())
	r.abortChan = make(chan os.Signal, 1)
	signal.Notify(r.abortChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go r.abort()
	go r.listenU()
	return r
}

func (r *Receiver) abort() {
	for range r.abortChan {
		if err := r.abortSign(); err != nil {
			fmt.Println(err)
		}
		r.cancel()
	}
}

func (r *Receiver) abortSign() error {
	if r.tranConn != nil {
		abortSign := make([]byte, 8)
		binary.BigEndian.PutUint32(abortSign, uint32(msg.TcpAbort))
		_, err := r.tranConn.Write(abortSign)
		return err
	}
	return nil
}

func (r *Receiver) Run() {
	for notice := range r.noticeChan {
		if r.getChoice(notice) {
			l, err := r.listenT(notice)
			if err != nil {
				fmt.Println(err)
				continue
			}
			go func() {
				data := make([]byte, 0, 8)
				r.tickSend(binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(data, msg.UdpAcK),
					uint32(l.Addr().(*net.TCPAddr).Port)), notice.remoteAddr)
			}()
			if err = r.serve(l); err != nil {
				fmt.Println("\nReceiving error:", err)
			} else {
				fmt.Println("RECEIVE SUCCESS!!!")
			}
			break
		} else {
			fmt.Println("Waiting for receiving ...")
			r.receiving = !r.receiving
		}
	}
}

func (r *Receiver) getChoice(info *info) bool {
	var choice string
	fmt.Printf("Do you want to receive %s? \nPlease input your choice, Y/y or N/n ?", info.fileName)
	fmt.Scanln(&choice)
	res := strings.TrimSpace(strings.ToLower(choice))
	switch res {
	case "y":
		return true
	default:
		return false
	}
}

func (r *Receiver) listenU() {
	var err error
	r.udpConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: global.ReceiverPort})
	if err != nil {
		log.Println(err)
		return
	}
	fmt.Println("Waiting for receiving ...")
	for {
		data := make([]byte, 1024)
		n, remoteAddr, err := r.udpConn.ReadFromUDP(data)
		if err != nil {
			log.Printf("error during read: %s\n", err)
			continue
		}
		if r.receiving {
			continue
		}
		if binary.BigEndian.Uint32(data[:4]) == msg.UdpReq {
			r.fileInfo.name = string(data[4:n])
			r.noticeChan <- &info{localAddr: r.udpConn.LocalAddr().(*net.UDPAddr), remoteAddr: remoteAddr, fileName: r.fileInfo.name}
			r.receiving = !r.receiving
		}
	}
}

func (r *Receiver) tickSend(data []byte, remoteAddr *net.UDPAddr) {
	_, err := r.udpConn.WriteToUDP(data, remoteAddr)
	if err != nil {
		fmt.Println(err)
	}
}

func (r *Receiver) listenT(msg *info) (*net.TCPListener, error) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: msg.localAddr.IP})
	if err != nil {
		return nil, err
	}
	return l, nil
}

func (r *Receiver) serve(l net.Listener) error {
	defer l.Close()
	conn, err := l.Accept()
	if err != nil {
		return err
	}
	r.tranConn = conn
	defer conn.Close()
LOOP:
	for {
		headData := make([]byte, r.packer.GetHeadLen())
		_, err = io.ReadFull(conn, headData)
		if err != nil {
			return err
		}
		message, err := r.packer.Unpack(headData)
		if err != nil {
			return err
		}
		switch message.ID {
		case msg.TcpFileMd5:
			if err = r.handleMd5Msg(conn, message); err != nil {
				return err
			}
		case msg.TcpFileSize:
			if err = r.handleSizeMsg(conn, message); err != nil {
				return err
			}
		case msg.TcpFile:
			if err = r.handleFileMsg(r.ctx, conn); err != nil {
				defer os.Remove(r.fileInfo.name)
				return err
			}
		case msg.TcpOver:
			break LOOP
		default:
			n, err := io.CopyN(io.Discard, conn, int64(message.GetDataLen()))
			if err != nil {
				return err
			}
			fmt.Println("discard: ", n)
		}
	}
	if err := r.over(); err != nil {
		return err
	}

	if !bytes.Equal(r.fileInfo.calMd5.Sum(nil), r.fileInfo.md5) {
		defer os.Remove(r.fileInfo.name)
		fmt.Println("file check failed,please retry!!!")
	}
	return nil
}

func (r *Receiver) over() error {
	overSign := make([]byte, 8)
	binary.BigEndian.PutUint32(overSign, uint32(msg.TcpOver))
	_, err := r.tranConn.Write(overSign)
	return err
}

func (r *Receiver) handleFileMsg(ctx context.Context, conn net.Conn) error {
	f, err := os.Create(r.fileInfo.name)
	if err != nil {
		log.Println("create file err:", err)
		return err
	}
	defer f.Close()
	bar := progressbar.DefaultBytes(
		r.fileInfo.size,
		fmt.Sprintf("%s Receiving", r.fileInfo.name),
	)
	_, err = copier.CopyN(ctx, io.MultiWriter(f, bar, r.fileInfo.calMd5), conn, r.fileInfo.size)
	return err
}

func (r *Receiver) handleMd5Msg(conn net.Conn, msg *msg.Message) error {
	msg.Data = make([]byte, msg.GetDataLen())
	_, err := io.ReadFull(conn, msg.Data)
	if err != nil {
		log.Println("server unpack data err:", err)
		return err
	}
	r.fileInfo.md5 = msg.GetData()
	return nil
}

func (r *Receiver) handleSizeMsg(conn net.Conn, msg *msg.Message) error {
	msg.Data = make([]byte, msg.GetDataLen())
	_, err := io.ReadFull(conn, msg.Data)
	if err != nil {
		log.Println("server unpack data err:", err)
		return err
	}
	r.fileInfo.size = int64(binary.BigEndian.Uint64(msg.GetData()))
	return nil
}
