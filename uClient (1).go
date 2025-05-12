package main

import (
	"bufio"
	"log"
	"net"
	"os"
)

//client entry point
func main() {
	
	//resolve server address
	serverAddr, err := net.ResolveUDPAddr("udp", "localhost:8080")
	if err != nil{
		log.Fatal(err)
	}

	//create client connection with random local port
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil{
		log.Fatal(err)
	}
	defer conn.Close()

	log.Printf("Connected on %s", conn.LocalAddr())

	//start message receiver in background
	go receiveMessages(conn)

	//handle user input in foreground
	sendMessages(conn, serverAddr)
}

//receiveMessages listens for incoming server broadcasts
func receiveMessages(conn *net.UDPConn){
	buffer := make([]byte, 1024)
	for{
		//read from UDP connection
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("Receive error: %v", err)
			continue
		}
		log.Printf("Received: %s", string(buffer[:n]))
	}
}

//sendMessages reads user input and sends to server
func sendMessages(conn *net.UDPConn, serverAddr *net.UDPAddr){

	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		//send each line of input to server
		if _, err := conn.WriteToUDP(scanner.Bytes(), serverAddr); err != nil{
			log.Printf("Send error: %v", err)
		}
	}
}