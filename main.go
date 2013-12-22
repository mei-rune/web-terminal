package main

import (
	"bitbucket.org/kardianos/osext"
	"bytes"
	"code.google.com/p/go.crypto/ssh"
	"code.google.com/p/go.net/websocket"
	"code.google.com/p/mahonia"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

var (
	listen = flag.String("listen", ":37079", "the port of http")
	debug  = flag.Bool("debug", false, "show debug message.")

	commands = map[string]string{}
)

type warpWriter struct {
	nm      string
	out     io.Writer
	buf     []byte
	decoder mahonia.Decoder
}

func (w *warpWriter) Write(p []byte) (c int, e error) {
	if 0 == len(w.buf) {
		n, cdata, e := w.decoder.Translate(p, false)
		if nil != e {
			return 0, e
		}
		if n != len(p) {
			w.buf = append(w.buf, p[n:]...)
		}
		os.Stdout.Write(cdata)
		if _, e = w.out.Write(cdata); nil != e {
			return 0, e
		}
		return len(p), nil
	} else {
		w.buf = append(w.buf, p...)
		n, cdata, e := w.decoder.Translate(w.buf, false)
		if nil != e {
			return 0, e
		}
		if n == len(w.buf) {
			w.buf = w.buf[0:0]
		} else {
			w.buf = w.buf[n:]
		}
		os.Stdout.Write(cdata)
		if _, e = w.out.Write(cdata); nil != e {
			return 0, e
		}
		return len(p), nil
	}
}

func warpW(nm string, dst io.Writer) io.Writer {
	//if *debug {
	return &warpWriter{nm: nm, out: dst, buf: make([]byte, 0, 8), decoder: mahonia.GetCharset("GB18030").NewDecoder()}
	//} else {
	//	return dst
	//}
}

type warpReader struct {
	nm string
	in io.Reader
}

func (w *warpReader) Read(p []byte) (int, error) {
	c, e := w.in.Read(p)
	if nil != e {
		fmt.Println(e)
	} else {
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

	session.Stdout = ws
	session.Stderr = ws
	session.Stdin = ws
	if err := session.Shell(); nil != err {
		logString(ws, "Unable to execute command:"+err.Error())
		return
	}
	if err := session.Wait(); nil != err {
		logString(ws, "Unable to execute command:"+err.Error())
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

	if _, err := io.Copy(ws, warp("client:", client)); err != nil {
		logString(ws, "copy of stdout failed:"+err.Error())
		return
	}
}

func ExecShell(ws *websocket.Conn) {
	defer ws.Close()
	pa := ws.Request().URL.Query().Get("exec")
	args := make([]string, 0, 10)
	vars := ws.Request().URL.Query()
	for i := 0; i < 1000; i++ {
		arguments, ok := vars["arg"+strconv.FormatInt(int64(i), 10)]
		if !ok {
			break
		}
		for _, argument := range arguments {
			args = append(args, argument)
		}
	}

	if c, ok := commands[pa]; ok {
		pa = c
	}
	cmd := exec.Command(pa, args...)
	// os_env := os.Environ()
	// environments := make([]string, 0, 1+len(os_env))
	// environments = append(environments, os_env...)
	// environments = append(environments, "PROCMGR_ID="+os.Args[0])
	// cmd.Env = environments
	cmd.Stderr = warpW("server:", ws)
	cmd.Stdout = cmd.Stderr
	if e := cmd.Run(); nil != e {
		io.WriteString(ws, e.Error())
	}
}

func abs(s string) string {
	r, e := filepath.Abs(s)
	if nil != e {
		return s
	}
	return r
}

func lookPath(executableFolder, pa string) (string, bool) {
	for _, nm := range []string{pa, pa + ".exe", pa + ".bat", pa + ".com"} {
		files := []string{nm,
			filepath.Join("bin", nm),
			filepath.Join("tools", nm),
			filepath.Join("..", nm),
			filepath.Join("..", "bin", nm),
			filepath.Join("..", "tools", nm),
			filepath.Join(executableFolder, nm),
			filepath.Join(executableFolder, "bin", nm),
			filepath.Join(executableFolder, "tools", nm),
			filepath.Join(executableFolder, "..", nm),
			filepath.Join(executableFolder, "..", "bin", nm),
			filepath.Join(executableFolder, "..", "tools", nm)}
		for _, file := range files {
			if st, e := os.Stat(file); nil == e && nil != st && !st.IsDir() {
				return abs(file), true
			}
		}
	}
	return "", false
}

func fillCommands(executableFolder string) {
	for _, nm := range []string{"snmpget", "snmpgetnext", "snmpdf", "snmpbulkget",
		"snmpbulkwalk", "snmpdelta", "snmpnetstat", "snmpset", "snmpstatus",
		"snmptable", "snmptest", "snmptools", "snmptranslate", "snmptrap", "snmpusm",
		"snmpvacm", "snmpwalk"} {
		if pa, ok := lookPath(executableFolder, nm); ok {
			commands[nm] = pa
		}
	}
}

func main() {
	flag.Parse()

	executableFolder, e := osext.ExecutableFolder()
	if nil != e {
		fmt.Println(e)
		return
	}

	fillCommands(executableFolder)

	files := []string{"web-terminal",
		filepath.Join("lib", "web-terminal"),
		filepath.Join("..", "lib", "web-terminal"),
		filepath.Join(executableFolder, "static"),
		filepath.Join(executableFolder, "web-terminal"),
		filepath.Join(executableFolder, "lib", "web-terminal"),
		filepath.Join(executableFolder, "..", "lib", "web-terminal")}
	file := ""
	for _, nm := range files {
		if st, e := os.Stat(nm); nil == e && nil != st && st.IsDir() {
			file = nm
			break
		}
	}
	if "" == file {
		buffer := bytes.NewBuffer(make([]byte, 0, 2048))
		buffer.WriteString("[warn] root path is not found:\r\n")
		for _, nm := range files {
			buffer.WriteString("\t\t")
			buffer.WriteString(nm)
			buffer.WriteString("\r\n")
		}
		buffer.Truncate(buffer.Len() - 2)
		log.Println(buffer)
		return
	}

	http.Handle("/ssh", websocket.Handler(SSHShell))
	http.Handle("/telnet", websocket.Handler(TelnetShell))
	http.Handle("/cmd", websocket.Handler(ExecShell))
	//http.Handle("/", http.FileServer(http.Dir(filepath.Join(executableFolder, "static"))))
	http.Handle("/", http.FileServer(http.Dir(file)))
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(file))))
	fmt.Println("[web-terminal] listen at '" + *listen + "' with root is '" + file + "'")
	err := http.ListenAndServe(*listen, nil)
	if err != nil {
		fmt.Println("ListenAndServe: " + err.Error())
	}
}
