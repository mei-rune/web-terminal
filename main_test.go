package main

import (
	"bytes"
	"code.google.com/p/mahonia"
	//"encoding/hex"
	//"fmt"
	"testing"
)

func TestDecodeWriter(t *testing.T) {
	var buf1 bytes.Buffer
	_, e := mahonia.GetCharset("GB18030").NewEncoder().NewWriter(&buf1).Write([]byte("中国中国中国中国中国"))
	txt := buf1.Bytes()
	if nil != e {
		t.Error(e)
		return
	}
	//fmt.Println(hex.EncodeToString(txt))

	var buf bytes.Buffer
	w := decodeBy("GB18030", &buf)
	for _, b := range txt {
		//fmt.Println("=====", idx, hex.EncodeToString([]byte{b}))
		w.Write([]byte{b})
	}
	if "中国中国中国中国中国" != buf.String() {
		t.Error("excepted is 中国中国中国中国中国, actual is", buf.String())
	}

	buf.Reset()
	w = decodeBy("GB18030", &buf)
	w.Write(txt[:1])
	w.Write(txt[1:3])
	w.Write(txt[3:])

	if "中国中国中国中国中国" != buf.String() {
		t.Error("excepted is 中国中国中国中国中国, actual is", buf.String())
	}

}
