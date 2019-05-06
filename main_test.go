package terminal

import (
	"flag"
	"io"
	"os"

	"golang.org/x/net/websocket"

	"testing"
)

func TestExecShell(t *testing.T) {
	*listen = ":7079"
	flag.Set("listen", ":7079")
	go main()

	ws, e := websocket.Dial("ws://192.168.1.142/hengwei/internal/terminal/cmd?exec=winrs&arg0=-r:192.168.1.2:5985&arg1=-u:WORKGROUP%5Cadministrator&arg2=-p:tpt_8498b2c7&arg3=net%20use%20%5C%5C192.168.1.142%5Cmy_script%20tpt_8498b2c7%20/USER:administrator%20/PERSISTENT:YES%20&&%20%5C%5C192.168.1.142%5Cmy_script%5Cecho.bat", "",
		"http://192.168.1.142/hengwei/internal/terminal/cmd?exec=winrs&arg0=-r:192.168.1.2:5985&arg1=-u:WORKGROUP%5Cadministrator&arg2=-p:tpt_8498b2c7&arg3=net%20use%20%5C%5C192.168.1.142%5Cmy_script%20tpt_8498b2c7%20/USER:administrator%20/PERSISTENT:YES%20&&%20%5C%5C192.168.1.142%5Cmy_script%5Cecho.bat")
	if nil != e {
		t.Error(e)
		return
	}

	go func() {
		if _, e := io.Copy(ws, os.Stdin); nil != e {
			t.Error(e)
		}
	}()

	io.Copy(os.Stdout, ws)
}
