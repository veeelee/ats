package main

import (
	//"cline"
	"command"
	//"configuration"
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"rut"
	"strconv"
)

var CTX = context.Background()

var IP = flag.String("ip", "10.71.20.149", "IP address of the remote device")
var Host = flag.String("hostname", "SWITCH", "Host name of the remote device")
var User = flag.String("username", "admin", "Username of the remote device")
var Password = flag.String("password", "", "Passwrod of the remote device")
var Start = flag.String("start", "", "start index")
var End = flag.String("end", "", "end index")

func main() {
	flag.Parse()

	ip := net.ParseIP(*IP)
	if ip == nil {
		fmt.Printf("Invalid IP address: %s\n", *IP)
		return
	}

	if *Host == "" {
		fmt.Println("Invalid Host name")
		return
	}

	if *User == "" {
		fmt.Println("Invalid username")
		return
	}

	var si int64
	var ei int64
	if *Start != "" {
		s, err := strconv.ParseInt(*Start, 10, 32)
		if err != nil {
			fmt.Println("Invalid start index to dump")
			return
		}
		if s < 0 {
			fmt.Println("Invalid start index to dump")
			return
		}
		si = s
	} else {
		si = 0
	}

	if *End != "" {
		e, err := strconv.ParseInt(*End, 10, 32)
		if err != nil {
			fmt.Println("Invalid end index to dump")
			return
		}
		if e < 0 {
			fmt.Println("Invalid end index to dump")
			return
		}
		ei = e
	} else {
		ei = 2047
	}

	if si > ei {
		fmt.Println("Start index should be smaller than End index")
		return
	}

	dev, err := rut.New(&rut.RUT{
		Name:     "V2708M",
		Device:   "V2",
		IP:       *IP,
		Port:     "23",
		Username: *User,
		Hostname: *Host,
		Password: *Password,
	})

	if err != nil {
		panic(err)
	}

	err = dev.Init()
	if err != nil {
		panic(err)
	}

	data, err := dev.RunCommand(CTX, &command.Command{
		Mode: "normal",
		CMD:  " config terminal",
	})
	if err != nil {
		fmt.Println(err)
	}

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "config",
		CMD:  " show running-config",
	})
	if err != nil {
		fmt.Println(err)
	}

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "config",
		CMD:  " do q sh -l",
	})
	if err != nil {
		fmt.Println(err)
	}

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " diag",
	})
	if err != nil {
		fmt.Println(err)
	}

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "rtkshell",
		CMD:  " terminal set pager length 0",
	})
	if err != nil {
		fmt.Println(err)
	}

	/*
		data, err = dev.RunCommand(CTX, &command.Command{
			Mode: "rtkshell",
			CMD:  " port dump port all",
		})
		if err != nil {
			fmt.Println(err)
		}

		fmt.Println(string(data))
	*/

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "rtkshell",
		CMD:  "register get all",
	})
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(string(data))
}

// hello world, the web server
func Main(w http.ResponseWriter, req *http.Request) {
	content, err := ioutil.ReadFile("index.html")
	if err != nil {
		io.WriteString(w, "Cannot open the file!\n")
	}
	w.Write(content)
}

/*
func init() {
	http.HandleFunc("/", Main)
	http.ListenAndServe(":9090", nil)
}
*/
