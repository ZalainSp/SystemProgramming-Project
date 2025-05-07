package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
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

	// Ask for and read username
	fmt.Fprint(conn, "Enter your name: ")
	nameReader := bufio.NewReader(conn)
	rawName, err := nameReader.ReadString('\n')
	if err != nil {
		return
	}
	username := strings.TrimSpace(rawName)

	//register client
	mutex.Lock()
	clients[conn] = true
	names[conn] = username
	mutex.Unlock()

	//announce join
	broadcast <- Message{
		name:   "Server",
		text:   fmt.Sprintf("%s has joined the chat\n", username),
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

	//client disconnects
	mutex.Lock()
	delete(clients, conn)
	delete(names, conn)
	mutex.Unlock()

	broadcast <- Message{
		name:   "Server",
		text:   fmt.Sprintf("%s has left the chat\n", username),
		sender: nil,
	}
}

func broadcaster() {
	for {
		m := <-broadcast
		mutex.Lock()
		for conn := range clients {
			// skip echoing back to the originator when its a user message
			if m.sender != nil && conn == m.sender {
				continue
			}
			//prefix server messages with [Server], others with [username]
			prefix := fmt.Sprintf("[%s] ", m.name)
			fmt.Fprint(conn, prefix+m.text)
		}
		mutex.Unlock()
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
