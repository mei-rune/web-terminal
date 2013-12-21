package main

import (
	"bitbucket.org/kardianos/osext"
	"code.google.com/p/go.crypto/ssh"
	"code.google.com/p/go.net/websocket"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

var (
	listen = flag.String("listen", ":37079", "the port of http")
	debug  = flag.Bool("debug", false, "show debug message.")
)

type warpReader struct {
	nm string
	in io.Reader
}

// func init() {
// 	executableFolder, err := osext.ExecutableFolder()
// 	if nil != err {
// 		panic(err)
// 		return
// 	}

// 	out, err = os.OpenFile(executableFolder+"/log.txt", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 666)
// 	if err != nil {
// 		panic(err)
// 		return
// 	}
// }

func (w *warpReader) Read(p []byte) (int, error) {
	c, e := w.in.Read(p)
	if nil != e {
		fmt.Println(e)
	} else {
		//out.Write(p[:c])
		fmt.Println(w.nm)
		os.Stdout.Write(p[:c])
	}
	return c, e
}

func warp(nm string, src io.Reader) io.Reader {
	if *debug {
		return &warpReader{nm: nm, in: src}
	} else {
		return src
	}
}

// password implements the ClientPassword interface
type password string

func (p password) Password(user string) (string, error) {
	return string(p), nil
}

func toInt(s string, v int) int {
	if value, e := strconv.ParseInt(s, 10, 0); nil == e {
		return int(value)
	}
	return v
}

func logString(ws io.Writer, msg string) {
	if nil != ws {
		io.WriteString(ws, "%tpt%"+msg)
	}
	log.Println(msg)
}

func SSHShell(ws *websocket.Conn) {
	defer ws.Close()
	hostname := ws.Request().URL.Query().Get("hostname")
	port := ws.Request().URL.Query().Get("port")
	if "" == port {
		port = "22"
	}
	user := ws.Request().URL.Query().Get("user")
	pwd := ws.Request().URL.Query().Get("password")
	columns := toInt(ws.Request().URL.Query().Get("columns"), 80)
	rows := toInt(ws.Request().URL.Query().Get("rows"), 40)

	// Dial code is taken from the ssh package example
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.ClientAuth{
			ssh.ClientAuthPassword(password(pwd)),
		},
	}
	client, err := ssh.Dial("tcp", hostname+":"+port, config)
	if err != nil {
		logString(ws, "Failed to dial: "+err.Error())
		return
	}

	session, err := client.NewSession()
	if err != nil {
		logString(ws, "Failed to create session: "+err.Error())
		return
	}
	defer session.Close()

	// Set up terminal modes
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}
	// Request pseudo terminal
	if err = session.RequestPty("xterm", columns, rows, modes); err != nil {
		logString(ws, "request for pseudo terminal failed:"+err.Error())
		return
	}
	stdin, err := session.StdinPipe()
	if nil != err {
		logString(ws, "create stdin failed:"+err.Error())
		return
	}
	stdout, err := session.StdoutPipe()
	if nil != err {
		logString(ws, "create stdou failed:"+err.Error())
		return
	}
	if err := session.Shell(); err != nil {
		logString(ws, "Unable to execute command:"+err.Error())
		return
	}
	go func() {
		_, err := io.Copy(stdin, warp("client:", ws))
		if err != nil {
			logString(nil, "copy of stdin failed:"+err.Error())
		}
		stdin.Close()
	}()
	if _, err := io.Copy(ws, warp("server:", stdout)); err != nil {
		logString(ws, "copy of stdout failed:"+err.Error())
		return
	}
}

func TelnetShell(ws *websocket.Conn) {
	defer ws.Close()
	hostname := ws.Request().URL.Query().Get("hostname")
	port := ws.Request().URL.Query().Get("port")
	if "" == port {
		port = "23"
	}
	//columns := toInt(ws.Request().URL.Query().Get("columns"), 80)
	//rows := toInt(ws.Request().URL.Query().Get("rows"), 40)
	client, err := net.Dial("tcp", hostname+":"+port)
	if nil != err {
		logString(ws, "Failed to dial: "+err.Error())
		return
	}
	defer func() {
		client.Close()
	}()
	go func() {
		_, err := io.Copy(client, warp("client:", ws))
		if nil != err {
			logString(nil, "copy of stdin failed:"+err.Error())
		}
	}()

	if _, err := io.Copy(ws, warp("server:", client)); err != nil {
		logString(ws, "copy of stdout failed:"+err.Error())
		return
	}
	time.Sleep(1 * time.Minute)
}

func main() {
	flag.Parse()

	executableFolder, e := osext.ExecutableFolder()
	if nil != e {
		fmt.Println(e)
		return
	}

	http.Handle("/ssh", websocket.Handler(SSHShell))
	http.Handle("/telnet", websocket.Handler(TelnetShell))
	//http.Handle("/", http.FileServer(http.Dir(filepath.Join(executableFolder, "static"))))
	http.Handle("/static/", http.FileServer(http.Dir(executableFolder)))
	fmt.Println("[web-terminal] listen at '" + *listen + "'")
	err := http.ListenAndServe(*listen, nil)
	if err != nil {
		fmt.Println("ListenAndServe: " + err.Error())
	}
}
