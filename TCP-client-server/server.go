package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type Message struct {
	name   string
	text   string
	sender net.Conn
}

var (
	clients   = make(map[net.Conn]bool)
	names     = make(map[net.Conn]string)
	mutex     sync.Mutex
	broadcast = make(chan Message)
)

func handleConnection(conn net.Conn) {
	defer conn.Close()

	//ask for and read username
	fmt.Fprint(conn, "Enter your name: ")
	nameReader := bufio.NewReader(conn)
	rawName, err := nameReader.ReadString('\n')
	if err != nil {
		return
	}
	username := strings.TrimSpace(rawName)

	//get the current login time
	loginTime := time.Now().Format("15:04:05")

	//register client
	mutex.Lock()
	clients[conn] = true
	names[conn] = username
	mutex.Unlock()

	//announce join
	broadcast <- Message{
		name:   "Server",
		text:   fmt.Sprintf("%s has joined the chat at %s\n", username, loginTime),
		sender: nil,
	}

	//read loop
	reader := bufio.NewReader(conn)
	for {
		msg, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		broadcast <- Message{
			name:   username,
			text:   msg,
			sender: conn,
		}
	}
	//get current disconnect time
	disconnectTime := time.Now().Format("15:04:05")

	//client disconnects
	mutex.Lock()
	delete(clients, conn)
	delete(names, conn)
	mutex.Unlock()

	broadcast <- Message{
		name:   "Server",
		text:   fmt.Sprintf("%s has left the chat at %s\n", username, disconnectTime),
		sender: nil,
	}
}

func broadcaster() {
	for {
		m := <-broadcast
		start := time.Now() //start timer for latency
		mutex.Lock()
		for conn := range clients {
			//skip echoing back to the sender when its a user message
			if m.sender != nil && conn == m.sender {
				continue
			}
			//prefix server messages with [Server] and others with [username]
			prefix := fmt.Sprintf("[%s] ", m.name)
			messageText := prefix + m.text
			fmt.Fprint(conn, messageText)

			//log message to file named after receiver's IP address
			clientAddr := conn.RemoteAddr().String()
			clientIP := strings.Split(clientAddr, ":")[0]
			filename := fmt.Sprintf("%s.log", clientIP)

			go func(entry, file string) {
				f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err == nil {
					defer f.Close()
					f.WriteString(entry)
				}
			}(messageText, filename)
		}
		mutex.Unlock()

		elapsed := time.Since(start) //end timer
		fmt.Printf("Broadcast latency: %v\n", elapsed) //log latency
	}
}


func main() {
	listener, err := net.Listen("tcp", ":4000")
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	go broadcaster()
	fmt.Println("TCP chat server started on port 4000...")

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleConnection(conn)
	}
}
