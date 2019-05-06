package main

import (
	"flag"
	"log"
	"net/http"

	terminal "github.com/runner-mei/web-terminal"
)

func main() {
	var appRoot, listen string

	flag.StringVar(&listen, "listen", ":37079", "the port of http")
	flag.StringVar(&appRoot, "url_prefix", "/", "url 前缀")
	flag.Parse()
	if nil != flag.Args() && 0 != len(flag.Args()) {
		flag.Usage()
		return
	}

	h, err := terminal.New(appRoot)
	if err != nil {
		log.Println(err)
	}

	log.Println("[web-terminal] listen at '" + listen + "'")
	err = http.ListenAndServe(listen, h)
	if err != nil {
		log.Println("ListenAndServe: " + err.Error())
	}
}
