package main

import (
	"bufio"
	"fmt"
	"net"
	"sync"
)

var (
	clients   = make(map[net.Conn]bool)
	mutex     sync.Mutex
	broadcast = make(chan string)
)

func handleConnection(conn net.Conn) {
	defer conn.Close()

	mutex.Lock()
	clients[conn] = true
	mutex.Unlock()

	reader := bufio.NewReader(conn)

	for {
		msg, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		broadcast <- fmt.Sprintf("%s", msg)
	}

	mutex.Lock()
	delete(clients, conn)
	mutex.Unlock()
}

func broadcaster() {
	for {
		msg := <-broadcast
		mutex.Lock()
		for conn := range clients {
			fmt.Fprint(conn, msg)
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
