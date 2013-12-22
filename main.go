package main

import (
	"bitbucket.org/kardianos/osext"
	"bytes"
	"code.google.com/p/go.crypto/ssh"
	"code.google.com/p/go.net/websocket"
	"code.google.com/p/mahonia"
	//"encoding/hex"
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
	"runtime"
	"strconv"
	"strings"
)

var (
	listen = flag.String("listen", ":37079", "the port of http")
	debug  = flag.Bool("debug", false, "show debug message.")

	commands = map[string]string{}
)

type consoleWriter struct {
	out io.Writer
}

func (w *consoleWriter) Write(p []byte) (c int, e error) {
	fmt.Println(p)
	return w.out.Write(p)
}

type decodeWriter struct {
	out     io.Writer
	buf     [8]byte
	length  int
	decoder mahonia.Decoder
}

func (w *decodeWriter) Write(p []byte) (c int, e error) {
	data := p
	if 0 != w.length {
		if len(p) <= 8-w.length {
			copy(w.buf[w.length:], p)
			w.length += len(p)
			n, cdata, e := w.decoder.Translate(w.buf[:w.length], false)
			if nil != e {
				return 0, e
			}
			if 0 == n {
				//fmt.Println(w.length)
				//fmt.Println(hex.EncodeToString(w.buf[:]))
				return len(p), nil
			}
			//fmt.Println(string(cdata))
			if _, e = w.out.Write(cdata); nil != e {
				return 0, e
			}
			w.length -= n

			if 0 != w.length {
				copy(w.buf[:], w.buf[n:])
			}
			//fmt.Println(w.length)
			//fmt.Println(hex.EncodeToString(w.buf[:]))
			return len(p), nil
		}
		old := w.length
		copy(w.buf[w.length:], data[:8-w.length])
		w.length = 8
		n, cdata, e := w.decoder.Translate(w.buf[:], false)
		if nil != e {
			return 0, e
		}
		if 0 == n {
			panic("n == 0?")
		}
		if old > n {
			panic("old > n?")
		}
		w.length -= n
		if nil != cdata {
			//fmt.Println(string(cdata))
			if _, e = w.out.Write(cdata); nil != e {
				return 0, e
			}
		}
		data = p[n-old:]
	}

	n, cdata, e := w.decoder.Translate(data, false)
	if nil != e {
		return 0, e
	}
	if nil != cdata {
		//fmt.Println(string(cdata))
		if _, e = w.out.Write(cdata); nil != e {
			return 0, e
		}
	}
	w.length = len(data) - n
	if 0 != w.length {
		if 8 <= w.length {
			panic("8 <= w.length?")
		}
		copy(w.buf[:], data[n:])
		w.length = len(data) - n
	}

	//fmt.Println(w.length)
	//fmt.Println(hex.EncodeToString(w.buf[:]))
	return len(p), nil
}

func decodeBy(charset string, dst io.Writer) io.Writer {
	if "UTF-8" == strings.ToUpper(charset) || "UTF8" == strings.ToUpper(charset) {
		return dst
	}
	//if *debug {
	return &decodeWriter{out: dst, decoder: mahonia.GetCharset(charset).NewDecoder()}
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
	charset := ws.Request().URL.Query().Get("charset")
	if "" == charset {
		if "windows" == runtime.GOOS {
			charset = "GB18030"
		} else {
			charset = "UTF-8"
		}
	}
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
	cmd.Stderr = decodeBy(charset, ws)
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
