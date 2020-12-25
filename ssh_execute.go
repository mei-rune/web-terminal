package terminal

import (
	"bytes"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

func linuxSSH(ws *websocket.Conn, args []string, charset, wd string, timeout time.Duration) {
	log.Println("begin to execute ssh:", args)

	// [ssh -batch -pw 8498b2c7 root@192.168.1.18 -m /var/lib/tpt/etc/scripts/abc.sh]
	flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
	_ = flagSet.Bool("batch", false, "")
	pw := flagSet.String("pw", "", "")
	idFile := flagSet.String("i", "", "")

	if err := flagSet.Parse(args); err != nil {
		io.WriteString(ws, "parse arguments error: "+err.Error())
		return
	}

	if len(flagSet.Args()) == 0 {
		io.WriteString(ws, "parse arguments error: command is missing")
		return
	}

	if args = flagSet.Args(); len(args) == 3 && args[1] == "-m" {
		bs, err := ioutil.ReadFile(args[2])
		if err != nil {
			io.WriteString(ws, "parse arguments error: command is missing")
			return
		}
		bs = bytes.TrimSpace(bs)
		if len(bs) == 0 {
			io.WriteString(ws, args[2]+" is empty")
			return
		}

		args = []string{args[0], string(bs)}
	}

	if *idFile != "" {
		args = append([]string{"-i", *idFile, "-o", "StrictHostKeyChecking=no"}, args...)
	} else {
		args = append([]string{"-o", "StrictHostKeyChecking=no"}, args...)
	}

	var output io.Writer = decodeBy(charset, ws)

	var cmd *exec.Cmd
	if *pw != "" {
		cmd = exec.Command("sshpass", append([]string{"-p", *pw, "ssh"}, args...)...)
	} else {
		cmd = exec.Command("ssh", args...)
	}
	if "" != wd {
		cmd.Dir = wd
	}

	cmd.Stdin = ws
	cmd.Stderr = output
	cmd.Stdout = output

	if err := cmd.Start(); err != nil {
		io.WriteString(ws, err.Error())
		return
	}

	go func() {
		defer recover()

		cmd.Process.Wait()
		ws.Close()
	}()

	timer := time.AfterFunc(timeout, func() {
		defer recover()
		cmd.Process.Kill()
	})

	if err := cmd.Wait(); err != nil {
		io.WriteString(ws, err.Error())
	}
	timer.Stop()
	ws.Close()
}

func Plink(ws *websocket.Conn) {
	defer ws.Close()
	hostname := ws.Request().URL.Query().Get("hostname")
	port := ws.Request().URL.Query().Get("port")
	if port != "" {
		hostname = net.JoinHostPort(hostname, port)
	}

	user := ws.Request().URL.Query().Get("user")
	pwd := ws.Request().URL.Query().Get("password")
	// columns := toInt(ws.Request().URL.Query().Get("columns"), 120)
	// rows := toInt(ws.Request().URL.Query().Get("rows"), 80)
	charset := ws.Request().URL.Query().Get("charset")
	if "" == charset {
		if "windows" == runtime.GOOS {
			charset = "GB18030"
		} else {
			charset = "UTF-8"
		}
	}

	pa := "plink"
	if c, ok := Commands[pa]; ok {
		pa = c
	}
	cmd := exec.Command(pa, "-pw", pwd, user+"@"+hostname)

	var combinedOut io.Writer = decodeBy(charset, ws)
	cmd.Stdout = combinedOut
	cmd.Stderr = combinedOut
	cmd.Stdin = ws

	if *is_debug || "true" == strings.ToLower(ws.Request().URL.Query().Get("debug")) {
		dump_out, err := os.OpenFile(filepath.Join(LogDir, strings.Replace(hostname, ":", "_", -1)+".dump_ssh_out.txt"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if nil == err {
			combinedOut = io.MultiWriter(dump_out, combinedOut)
		} else {
			defer dump_out.Close()
		}

		dump_in, err := os.OpenFile(filepath.Join(LogDir, strings.Replace(hostname, ":", "_", -1)+".dump_ssh_in.txt"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if nil != err {
			dump_in = nil
		} else {
			defer dump_in.Close()
		}
		cmd.Stdout = combinedOut
		cmd.Stderr = combinedOut
		cmd.Stdin = warp(ws, dump_in)
	}

	if err := cmd.Start(); err != nil {
		io.WriteString(ws, err.Error())
		return
	}

	go func() {
		defer recover()

		cmd.Process.Wait()
		ws.Close()
	}()

	timer := time.AfterFunc(1*time.Hour, func() {
		defer recover()
		cmd.Process.Kill()
	})

	if err := cmd.Wait(); err != nil {
		io.WriteString(ws, err.Error())
	}
	timer.Stop()
	ws.Close()
}
