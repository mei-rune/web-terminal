package main

import (
	"bufio"
	"bytes"
	"errors"
	"io/ioutil"
	"time"

	"golang.org/x/text/transform"

	"github.com/kardianos/osext"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/websocket"

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

	"github.com/fd/go-shellwords/shellwords"
)

var (
	sh_execute = "bash"
	listen     = flag.String("listen", ":37079", "the port of http")
	is_debug   = flag.Bool("debug", false, "show debug message.")
	mibs_dir   = flag.String("mibs_dir", "", "set mibs directory.")

	supportedCiphers = GetSupportedCiphers()
	commands         = map[string]string{}

	logs_dir         = ""
	ExecutableFolder string
)

func init() {
	flag.StringVar(&sh_execute, "sh_execute", "bash", "the shell path")
}

func GetSupportedCiphers() []string {
	config := &ssh.ClientConfig{}
	config.SetDefaults()
	for _, cipher := range []string{"aes128-cbc"} {
		found := false
		for _, defaultCipher := range config.Ciphers {
			if cipher == defaultCipher {
				found = true
				break
			}
		}

		if !found {
			config.Ciphers = append(config.Ciphers, cipher)
		}
	}
	return config.Ciphers
}

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

func decodeBy(charset string, dst io.Writer) io.Writer {
	if "UTF-8" == strings.ToUpper(charset) || "UTF8" == strings.ToUpper(charset) {
		return dst
	}
	cs := GetCharset(charset)
	if nil == cs {
		panic("charset '" + charset + "' is not exists.")
	}

	return transform.NewWriter(dst, cs.NewDecoder())
}

type matchWriter struct {
	out        io.Writer
	excepted   []byte
	buf        bytes.Buffer
	cb         func()
	is_matched bool
}

func (w *matchWriter) match(p []byte) {
	if len(p) > len(w.excepted) {
		if bytes.Contains(p, w.excepted) {
			w.is_matched = true
			w.buf.Reset()
			w.cb()
			return
		}
		w.buf.Write(p[:len(w.excepted)-1])

		if bytes.Contains(w.buf.Bytes(), w.excepted) {
			w.is_matched = true
			w.buf.Reset()
			w.cb()
			return
		}
		w.buf.Reset()
		w.buf.Write(p[len(p)-len(w.excepted):])
		return
	}
	w.buf.Write(p)
	if w.buf.Len() <= len(w.excepted) {
		return
	}

	if bytes.Contains(w.buf.Bytes(), w.excepted) {
		w.is_matched = true
		w.buf.Reset()
		w.cb()
		return
	}

	reserved := w.buf.Bytes()[w.buf.Len()-len(w.excepted):]
	copy(w.buf.Bytes(), reserved)
	w.buf.Truncate(len(reserved))
}

func (w *matchWriter) Write(p []byte) (c int, e error) {
	c, e = w.out.Write(p)
	if !w.is_matched {
		w.match(p)
	}
	return
}

func matchBy(dst io.Writer, excepted string, cb func()) io.Writer {
	return &matchWriter{
		out:      dst,
		excepted: []byte(excepted),
		cb:       cb,
	}
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
	var dump_out, dump_in io.WriteCloser
	defer func() {
		ws.Close()
		if nil != dump_out {
			dump_out.Close()
		}
		if nil != dump_in {
			dump_in.Close()
		}
	}()

	hostname := ws.Request().URL.Query().Get("hostname")
	port := ws.Request().URL.Query().Get("port")
	if "" == port {
		port = "22"
	}
	user := ws.Request().URL.Query().Get("user")
	pwd := ws.Request().URL.Query().Get("password")
	columns := toInt(ws.Request().URL.Query().Get("columns"), 120)
	rows := toInt(ws.Request().URL.Query().Get("rows"), 80)
	debug := *is_debug
	if "true" == strings.ToLower(ws.Request().URL.Query().Get("debug")) {
		debug = true
	}

	password_count := 0
	empty_interactive_count := 0
	reader := bufio.NewReader(ws)
	// Dial code is taken from the ssh package example
	config := &ssh.ClientConfig{
		Config:          ssh.Config{Ciphers: supportedCiphers},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		User:            user,
		Auth: []ssh.AuthMethod{
			ssh.Password(pwd),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
				if len(questions) == 0 {
					empty_interactive_count++
					if empty_interactive_count++; empty_interactive_count > 50 {
						return nil, errors.New("interactive count is too much")
					}
					return []string{}, nil
				}
				for _, question := range questions {
					io.WriteString(ws, question)

					switch strings.ToLower(strings.TrimSpace(question)) {
					case "password:", "password as":
						password_count++
						if password_count == 1 {
							answers = append(answers, pwd)
							break
						}
						fallthrough
					default:
						line, _, e := reader.ReadLine()
						if nil != e {
							return nil, e
						}
						answers = append(answers, string(line))
					}
				}
				return answers, nil
			}),
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
	if err = session.RequestPty("xterm", rows, columns, modes); err != nil {
		logString(ws, "request for pseudo terminal failed:"+err.Error())
		return
	}

	var combinedOut io.Writer = ws
	if debug {
		dump_out, err = os.OpenFile(logs_dir+hostname+".dump_ssh_out.txt", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if nil == err {
			combinedOut = io.MultiWriter(dump_out, ws)
		}

		dump_in, err = os.OpenFile(logs_dir+hostname+".dump_ssh_in.txt", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if nil != err {
			dump_in = nil
		}
	}

	session.Stdout = combinedOut
	session.Stderr = combinedOut
	session.Stdin = warp(ws, dump_in)
	if err := session.Shell(); nil != err {
		logString(ws, "Unable to execute command:"+err.Error())
		return
	}
	if err := session.Wait(); nil != err {
		logString(ws, "Unable to execute command:"+err.Error())
	}
}

func SSHExec(ws *websocket.Conn) {
	var dump_out, dump_in io.WriteCloser
	defer func() {
		ws.Close()
		if nil != dump_out {
			dump_out.Close()
		}
		if nil != dump_in {
			dump_in.Close()
		}
	}()

	hostname := ws.Request().URL.Query().Get("hostname")
	port := ws.Request().URL.Query().Get("port")
	if "" == port {
		port = "22"
	}
	user := ws.Request().URL.Query().Get("user")
	pwd := ws.Request().URL.Query().Get("password")
	debug := *is_debug
	if "true" == strings.ToLower(ws.Request().URL.Query().Get("debug")) {
		debug = true
	}

	cmd := ws.Request().URL.Query().Get("cmd")
	cmd_alias := ws.Request().URL.Query().Get("dump_file")
	if "" == cmd_alias {
		cmd_alias = strings.Replace(cmd, " ", "_", -1)
	}

	password_count := 0
	empty_interactive_count := 0
	reader := bufio.NewReader(ws)
	// Dial code is taken from the ssh package example
	config := &ssh.ClientConfig{
		Config:          ssh.Config{Ciphers: supportedCiphers},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		User:            user,
		Auth: []ssh.AuthMethod{
			ssh.Password(pwd),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
				if len(questions) == 0 {
					empty_interactive_count++
					if empty_interactive_count++; empty_interactive_count > 50 {
						return nil, errors.New("interactive count is too much")
					}
					return []string{}, nil
				}
				for _, question := range questions {
					io.WriteString(ws, question)

					switch strings.ToLower(strings.TrimSpace(question)) {
					case "password:", "password as":
						password_count++
						if password_count == 1 {
							answers = append(answers, pwd)
							break
						}
						fallthrough
					default:
						line, _, e := reader.ReadLine()
						if nil != e {
							return nil, e
						}
						answers = append(answers, string(line))
					}
				}
				return answers, nil
			})},
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

	var combinedOut io.Writer = ws
	if debug {
		dump_out, err = os.OpenFile(logs_dir+hostname+"_"+cmd_alias+".dump_ssh_out.txt", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if nil == err {
			fmt.Println("log to file", logs_dir+hostname+"_"+cmd_alias+".dump_ssh_out.txt")
			combinedOut = io.MultiWriter(dump_out, ws)
		} else {
			fmt.Println("failed to open log file,", err)
		}

		dump_in, err = os.OpenFile(logs_dir+hostname+"_"+cmd_alias+".dump_ssh_in.txt", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if nil != err {
			dump_in = nil
			fmt.Println("failed to open log file,", err)
		} else {
			fmt.Println("log to file", logs_dir+hostname+"_"+cmd_alias+".dump_ssh_in.txt")
		}
	}

	session.Stdout = combinedOut
	session.Stderr = combinedOut
	session.Stdin = warp(ws, dump_in)

	if err := session.Start(cmd); nil != err {
		logString(combinedOut, "Unable to execute command:"+err.Error())
		return
	}
	if err := session.Wait(); nil != err {
		logString(combinedOut, "Unable to execute command:"+err.Error())
		return
	}
	fmt.Println("exec ok")
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

	var dump_out io.WriteCloser
	var dump_in io.WriteCloser

	client, err := net.Dial("tcp", hostname+":"+port)
	if nil != err {
		logString(ws, "Failed to dial: "+err.Error())
		return
	}
	defer func() {
		client.Close()
		if nil != dump_out {
			dump_out.Close()
		}
		if nil != dump_in {
			dump_in.Close()
		}
	}()

	debug := *is_debug
	if "true" == strings.ToLower(ws.Request().URL.Query().Get("debug")) {
		debug = true
	}

	if debug {
		var err error
		dump_out, err = os.OpenFile(logs_dir+hostname+".dump_telnet_out.txt", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if nil != err {
			dump_out = nil
		}
		dump_in, err = os.OpenFile(logs_dir+hostname+".dump_telnet_in.txt", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if nil != err {
			dump_in = nil
		}
	}

	conn, e := NewConnWithRead(client, warp(client, dump_in))
	if nil != e {
		logString(nil, "failed to create connection: "+e.Error())
		return
	}
	columns := toInt(ws.Request().URL.Query().Get("columns"), 80)
	rows := toInt(ws.Request().URL.Query().Get("rows"), 40)
	conn.setWindowSize(byte(rows), byte(columns))

	go func() {
		_, err := io.Copy(decodeBy(charset, client), warp(ws, dump_out))
		if nil != err {
			logString(nil, "copy of stdin failed:"+err.Error())
		}
	}()

	if _, err := io.Copy(decodeBy(charset, ws), conn); err != nil {
		logString(ws, "copy of stdout failed:"+err.Error())
		return
	}
}

func Replay(ws *websocket.Conn) {
	defer ws.Close()
	file_name := ws.Request().URL.Query().Get("file")
	charset := ws.Request().URL.Query().Get("charset")
	if "" == charset {
		if "windows" == runtime.GOOS {
			charset = "GB18030"
		} else {
			charset = "UTF-8"
		}
	}
	dump_out, err := os.Open(file_name)
	if nil != err {
		logString(ws, "open '"+file_name+"' failed:"+err.Error())
		return
	}
	defer dump_out.Close()

	if _, err := io.Copy(decodeBy(charset, ws), dump_out); err != nil {
		logString(ws, "copy of stdout failed:"+err.Error())
		return
	}
}

func ExecShell(ws *websocket.Conn) {
	defer ws.Close()

	query_params := ws.Request().URL.Query()
	wd := query_params.Get("wd")
	charset := query_params.Get("charset")
	pa := query_params.Get("exec")
	timeout := query_params.Get("timeout")
	stdin := query_params.Get("stdin")

	args := make([]string, 0, 10)
	for i := 0; i < 1000; i++ {
		arguments, ok := query_params["arg"+strconv.FormatInt(int64(i), 10)]
		if !ok {
			break
		}
		for _, argument := range arguments {
			args = append(args, argument)
		}
	}

	execShell(ws, pa, args, charset, wd, stdin, timeout)
}

func ExecShell2(ws *websocket.Conn) {
	defer ws.Close()

	query_params := ws.Request().URL.Query()
	wd := query_params.Get("wd")
	charset := query_params.Get("charset")
	pa := query_params.Get("exec")
	timeout := query_params.Get("timeout")
	stdin := query_params.Get("stdin")

	ss, e := shellwords.Split(pa)
	if nil != e {
		io.WriteString(ws, "命令格式不正确：")
		io.WriteString(ws, e.Error())
		return
	}
	pa = ss[0]
	args := ss[1:]

	execShell(ws, pa, args, charset, wd, stdin, timeout)
}

func removeBatchOption(args []string) []string {
	offset := 0
	for idx, s := range args {
		if strings.ToLower(s) == "-batch" {
			continue
		}
		if offset != idx {
			args[offset] = s
		}
		offset += 1
	}
	return args[:offset]
}

func addMibDir(args []string) []string {
	has_mibs_dir := false
	for _, argument := range args {
		if "-M" == argument {
			has_mibs_dir = true
		}
	}

	if !has_mibs_dir {
		new_args := make([]string, len(args)+2)
		new_args[0] = "-M"
		new_args[1] = *mibs_dir
		copy(new_args[2:], args)
		args = new_args
	}
	return args
}

func execShell(ws *websocket.Conn, pa string, args []string, charset, wd, stdin, timeout_str string) {
	if "" == charset {
		if "windows" == runtime.GOOS {
			charset = "GB18030"
		} else {
			charset = "UTF-8"
		}
	}

	timeout := 10 * time.Minute
	if "" != timeout_str {
		if t, e := time.ParseDuration(timeout_str); nil == e {
			timeout = t
		}
	}

	query_params := ws.Request().URL.Query()
	if _, ok := query_params["file"]; ok {
		file_content := query_params.Get("file")
		f, e := ioutil.TempFile(os.TempDir(), "run")
		if nil != e {
			io.WriteString(ws, "生成临时文件失败：")
			io.WriteString(ws, e.Error())
			return
		}

		filename := f.Name()
		defer func() {
			f.Close()
			os.Remove(filename)
		}()

		_, e = io.WriteString(f, file_content)
		if nil != e {
			io.WriteString(ws, "写临时文件失败：")
			io.WriteString(ws, e.Error())
			return
		}
		f.Close()

		args = append(args, filename)
	}

	if pa == "ssh" && runtime.GOOS != "windows" {
		linuxSSH(ws, args, charset, wd, timeout)
		return
	}

	if strings.HasPrefix(pa, "snmp") {
		args = addMibDir(args)
	} else if pa == "tpt" || pa == "tpt.exe" {
		args = append([]string{"-gbk=true"}, args...)
	}

	if c, ok := commands[pa]; ok {
		pa = c
	} else {
		if newPa, ok := lookPath(ExecutableFolder, pa); ok {
			pa = newPa
		}
	}

	is_connection_abandoned := false
	var output io.Writer = decodeBy(charset, ws)
	if pp := strings.ToLower(pa); strings.HasSuffix(pp, "plink.exe") || strings.HasSuffix(pp, "plink") {
		output = matchBy(output, "Connection abandoned.", func() {
			is_connection_abandoned = true
		})
	}

	cmd := exec.Command(pa, args...)
	if "" != wd {
		cmd.Dir = wd
	}
	if stdin == "on" {
		cmd.Stdin = ws
	}
	cmd.Stderr = output
	cmd.Stdout = output

	log.Println(cmd.Path, cmd.Args)

	if err := cmd.Start(); err != nil {

		if !os.IsPermission(err) || runtime.GOOS == "windows" {
			io.WriteString(ws, err.Error())
			return
		}

		newArgs := append(make([]string, len(args)+1))
		newArgs[0] = pa
		copy(newArgs[1:], args)
		cmd = exec.Command(sh_execute, newArgs...)
		if "" != wd {
			cmd.Dir = wd
		}
		cmd.Stdin = ws
		cmd.Stderr = output
		cmd.Stdout = output

		log.Println(cmd.Path, cmd.Args)
		if err := cmd.Start(); err != nil {
			io.WriteString(ws, err.Error())
			return
		}
	}

	timer := time.AfterFunc(timeout, func() {
		defer recover()
		cmd.Process.Kill()
	})

	if stdin == "on" {
		if state, err := cmd.Process.Wait(); err != nil {
			io.WriteString(ws, err.Error())
		} else if state != nil && !state.Success() {
			io.WriteString(ws, state.String())
		}
	} else {
		if err := cmd.Wait(); err != nil {
			io.WriteString(ws, err.Error())
		}
	}
	timer.Stop()
	if err := ws.Close(); err != nil {
		log.Println(err)
	}

	if is_connection_abandoned {
		saveSessionKey(pa, args, wd)
	}
}

func saveSessionKey(pa string, args []string, wd string) {
	args = removeBatchOption(args)
	var cmd = exec.Command(pa, args...)
	if "" != wd {
		cmd.Dir = wd
	}

	timer := time.AfterFunc(1*time.Minute, func() {
		defer recover()
		cmd.Process.Kill()
	})
	cmd.Stdin = strings.NewReader("y\ny\ny\ny\ny\ny\ny\ny\n")
	cmd.Run()
	timer.Stop()
}

func abs(s string) string {
	r, e := filepath.Abs(s)
	if nil != e {
		return s
	}
	return r
}

func lookPath(executableFolder string, alias ...string) (string, bool) {
	var names []string
	for _, aliasName := range alias {
		if runtime.GOOS == "windows" {
			names = append(names, aliasName, aliasName+".bat", aliasName+".com", aliasName+".exe")
		} else {
			names = append(names, aliasName, aliasName+".sh")
		}
	}

	for _, nm := range names {
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
			// fmt.Println("====", file)
			file = abs(file)
			if st, e := os.Stat(file); nil == e && nil != st && !st.IsDir() {
				//fmt.Println("1=====", file, e)
				return file, true
			}
		}
	}

	for _, nm := range names {
		_, err := exec.LookPath(nm)
		if nil == err {
			return nm, true
		}
	}
	return "", false
}

func fillCommands(executableFolder string) {
	for _, nm := range []string{"snmpget", "snmpgetnext", "snmpdf", "snmpbulkget",
		"snmpbulkwalk", "snmpdelta", "snmpnetstat", "snmpset", "snmpstatus",
		"snmptable", "snmptest", "snmptools", "snmptranslate", "snmptrap", "snmpusm",
		"snmpvacm", "snmpwalk", "wshell"} {
		if pa, ok := lookPath(executableFolder, nm); ok {
			commands[nm] = pa
		} else if pa, ok := lookPath(executableFolder, "netsnmp/"+nm); ok {
			commands[nm] = pa
		}
	}

	if pa, ok := lookPath(executableFolder, "tpt"); ok {
		commands["tpt"] = pa
	}
	if pa, ok := lookPath(executableFolder, "nmap/nping"); ok {
		commands["nping"] = pa
	}
	if pa, ok := lookPath(executableFolder, "nmap/nmap"); ok {
		commands["nmap"] = pa
	}
	if pa, ok := lookPath(executableFolder, "putty/plink", "ssh"); ok {
		commands["plink"] = pa
		commands["ssh"] = pa
	}
	if pa, ok := lookPath(executableFolder, "dig/dig", "dig"); ok {
		commands["dig"] = pa
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
	ExecutableFolder = executableFolder

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
		files = []string{"mibs",
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

	files = []string{"web-terminal",
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

	http.Handle("/replay", websocket.Handler(Replay))
	http.Handle("/ssh", websocket.Handler(SSHShell))
	http.Handle("/telnet", websocket.Handler(TelnetShell))
	http.Handle("/cmd", websocket.Handler(ExecShell))
	http.Handle("/cmd2", websocket.Handler(ExecShell2))
	http.Handle("/ssh_exec", websocket.Handler(SSHExec))

	//http.Handle("/", http.FileServer(http.Dir(filepath.Join(executableFolder, "static"))))
	http.Handle("/", http.FileServer(http.Dir(file)))
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(file))))
	fmt.Println("[web-terminal] listen at '" + *listen + "' with root is '" + file + "'")
	err := http.ListenAndServe(*listen, nil)
	if err != nil {
		fmt.Println("ListenAndServe: " + err.Error())
	}
}
