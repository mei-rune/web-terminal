package main

import (
	"fmt"
	"io"
	"net"
	"os"
)

type consoleReader struct {
	dst io.ReadCloser
	out io.Writer
}

func (w *consoleReader) Read(p []byte) (n int, err error) {
	n, err = w.dst.Read(p)
	if n > 0 {
		w.out.Write(p[:n])
	}
	return
}
func (w *consoleReader) Close() error {
	return w.dst.Close()
}

func warp(dst io.ReadCloser, dump io.Writer) io.ReadCloser {
	if nil == dump {
		return dst
	}
	return &consoleReader{out: dump, dst: dst}
}

func main() {
	tcp, e := net.Listen("tcp", ":23")
	if nil != e {
		fmt.Println(e)
		return
	}

	i := 0
	for {
		cl, e := tcp.Accept()
		if nil != e {
			fmt.Println(e)
			return
		}

		proxy, e := net.Dial("tcp", "34.2.0.1:23")
		if nil != e {
			cl.Close()
			fmt.Println(e)
			continue
		}

		i++
		dump_out, err := os.OpenFile(fmt.Sprint(i)+".dump_out.txt", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0)
		if nil != err {
			cl.Close()
			fmt.Println(e)
			continue
		}

		dump_in, err := os.OpenFile(fmt.Sprint(i)+".dump_in.txt", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0)
		if nil != err {
			cl.Close()
			fmt.Println(e)
			continue
		}

		go func() {
			io.Copy(io.MultiWriter(proxy, dump_out), cl)
		}()

		go func() {
			io.Copy(io.MultiWriter(cl, dump_in), proxy)
		}()
	}
}
