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
	listen   = flag.String("listen", ":37079", "the port of http")
	debug    = flag.Bool("debug", false, "show debug message.")
	mibs_dir = flag.String("mibs_dir", "", "set mibs directory.")

	commands = map[string]string{}

	logs_dir = ""
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
	cs := mahonia.GetCharset(charset)
	if nil == cs {
		panic("charset '" + charset + "' is not exists.")
	}
	//if *debug {
	return &decodeWriter{out: dst, decoder: cs.NewDecoder()}
	//} else {
	//	return dst
	//}
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
	var f io.WriteCloser
	defer func() {
		ws.Close()
		if nil != f {
			f.Close()
		}
	}()

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
		Auth: []ssh.AuthMethod{ssh.Password(pwd)},
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

	var combinedOut io.Writer = ws
	if *debug {
		f, err = os.OpenFile(logs_dir+hostname+".dump_ssh.txt", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0)
		if nil == err {
			combinedOut = io.MultiWriter(f, ws)
		} else {
			fmt.Println(hostname+".dump_ssh.txt is failed,", err)
		}
	}

	session.Stdout = combinedOut
	session.Stderr = combinedOut
	session.Stdin = warp(ws, f)
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
	charset := ws.Request().URL.Query().Get("charset")
	if "" == charset {
		if "windows" == runtime.GOOS {
			charset = "GB18030"
		} else {
			charset = "UTF-8"
		}
	}
	//columns := toInt(ws.Request().URL.Query().Get("columns"), 80)
	//rows := toInt(ws.Request().URL.Query().Get("rows"), 40)

	var f io.WriteCloser
	client, err := net.Dial("tcp", hostname+":"+port)
	if nil != err {
		logString(ws, "Failed to dial: "+err.Error())
		return
	}
	defer func() {
		client.Close()
		if nil != f {
			f.Close()
		}
	}()

	if *debug {
		var err error
		f, err = os.OpenFile(logs_dir+hostname+".dump_telnet.txt", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0)
		if nil != err {
			f = nil
		}
	}

	go func() {
		_, err := io.Copy(decodeBy(charset, client), warp(ws, f))
		if nil != err {
			logString(nil, "copy of stdin failed:"+err.Error())
		}
	}()

	if _, err := io.Copy(decodeBy(charset, ws), warp(client, f)); err != nil {
		logString(ws, "copy of stdout failed:"+err.Error())
		return
	}
}

func ExecShell(ws *websocket.Conn) {
	defer ws.Close()
	is_snmp_command := false
	has_mibs_dir := false
	pa := ws.Request().URL.Query().Get("exec")
	if strings.HasPrefix(pa, "snmp") {
		is_snmp_command = true
	}
	if c, ok := commands[pa]; ok {
		pa = c
	}

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
			if is_snmp_command && "-M" == argument {
				has_mibs_dir = true
			}
			args = append(args, argument)
		}
	}
	var cmd *exec.Cmd
	if is_snmp_command && !has_mibs_dir {
		cmd = exec.Command(pa)
		cmd.Args = append(cmd.Args, "-M")
		cmd.Args = append(cmd.Args, *mibs_dir)
		cmd.Args = append(cmd.Args, args...)
	} else {
		cmd = exec.Command(pa, args...)
	}
	cmd.Stdin = ws
	cmd.Stderr = decodeBy(charset, ws)
	cmd.Stdout = cmd.Stderr
	if err := cmd.Start(); err != nil {
		io.WriteString(ws, err.Error())
		return
	}

	if _, err := cmd.Process.Wait(); err != nil {
		io.WriteString(ws, err.Error())
	}
	ws.Close()
	cmd.Wait()
}

func abs(s string) string {
	r, e := filepath.Abs(s)
	if nil != e {
		return s
	}
	return r
}

func lookPath(executableFolder, pa string) (string, bool) {
	for _, nm := range []string{pa + ".bat", pa + ".com", pa, pa + ".exe"} {
		files := []string{nm,
			filepath.Join("bin", nm),
			filepath.Join("tools", nm),
			filepath.Join("runtime_env", nm),
			filepath.Join("..", nm),
			filepath.Join("..", "bin", nm),
			filepath.Join("..", "tools", nm),
			filepath.Join("..", "runtime_env", nm),
			filepath.Join(executableFolder, nm),
			filepath.Join(executableFolder, "bin", nm),
			filepath.Join(executableFolder, "tools", nm),
			filepath.Join(executableFolder, "runtime_env", nm),
			filepath.Join(executableFolder, "..", nm),
			filepath.Join(executableFolder, "..", "bin", nm),
			filepath.Join(executableFolder, "..", "tools", nm),
			filepath.Join(executableFolder, "..", "runtime_env", nm)}
		for _, file := range files {
			file = abs(file)
			if st, e := os.Stat(file); nil == e && nil != st && !st.IsDir() {
				return file, true
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

	if pa, ok := lookPath(executableFolder, "nmap/nping"); ok {
		commands["nping"] = pa
	}
	if pa, ok := lookPath(executableFolder, "nmap/nmap"); ok {
		commands["nmap"] = pa
	}
}

func main() {
	flag.Parse()
	if nil != flag.Args() && 0 != len(flag.Args()) {
		flag.Usage()
		return
	}

	executableFolder, e := osext.ExecutableFolder()
	if nil != e {
		fmt.Println(e)
		return
	}

	files := []string{"logs",
		filepath.Join("..", "logs"),
		filepath.Join(executableFolder, "logs"),
		filepath.Join(executableFolder, "..", "logs")}
	for _, nm := range files {
		nm = abs(nm)
		if st, e := os.Stat(nm); nil == e && nil != st && st.IsDir() {
			logs_dir = nm + "/"
			log.Println("'logs' directory is '" + logs_dir + "'")
			break
		}
	}

	if "" == *mibs_dir {
		files := []string{"mibs",
			filepath.Join("lib", "mibs"),
			filepath.Join("tools", "mibs"),
			filepath.Join("..", "lib", "mibs"),
			filepath.Join("..", "tools", "mibs"),
			filepath.Join(executableFolder, "mibs"),
			filepath.Join(executableFolder, "tools", "mibs"),
			filepath.Join(executableFolder, "lib", "mibs"),
			filepath.Join(executableFolder, "..", "lib", "mibs"),
			filepath.Join(executableFolder, "..", "tools", "mibs")}
		for _, nm := range files {
			nm = abs(nm)
			if st, e := os.Stat(nm); nil == e && nil != st && st.IsDir() {
				flag.Set("mibs_dir", nm)
				log.Println("'mibs' directory is '" + *mibs_dir + "'")
				break
			}
		}
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
		nm = abs(nm)
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
