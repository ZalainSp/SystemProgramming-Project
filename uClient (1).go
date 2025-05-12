//Writer: Jeronimo Tzib

package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	clientBufferSize = 2048 //increased buffer size
)

var clientSequenceNumber uint64 //atomically incremented for each message sent

func main(){

	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <server_address:port> (e.g., localhost:8080)", os.Args[0])
	}
	serverAddrStr := os.Args[1]

	serverAddr, err := net.ResolveUDPAddr("udp", serverAddrStr)
	if err != nil {
		log.Fatalf("Failed to resolve server address '%s': %v", serverAddrStr, err)
	}

	//create client connection with random local port
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil{
		log.Fatalf("Failed to listen on UDP: %v", err)
	}
	defer conn.Close()

	log.Printf("Client started. Connected to server via local port %s. Type messages and press Enter.", conn.LocalAddr().String())

	//start message receiver in background
	go receiveMessages(conn)

	//handle user input in foreground
	sendMessages(conn, serverAddr)
}

//receiveMessages listens for incoming server broadcasts
func receiveMessages(conn *net.UDPConn){

	buffer := make([]byte, clientBufferSize)

	for{
		n, remoteAddr, err := conn.ReadFromUDP(buffer) //remoteAddr here is the server's address

		if err != nil{
			if strings.Contains(err.Error(), "use of closed network connection"){
				log.Println("Client connection closed, stopping receiver.")
				return
			}
			log.Printf("Receive error: %v", err)
			continue
		}

		msgString := string(buffer[:n])
		parts := strings.SplitN(msgString, "|", 3)

		if len(parts) == 3{
			originalSenderSeqNum, errSeq := strconv.ParseUint(parts[0], 10, 64)
			clientSendTimestampNs, errTs := strconv.ParseInt(parts[1], 10, 64)
			payload := parts[2]

			if errSeq != nil || errTs != nil {
				log.Printf("Received (malformed header from %s): %s", remoteAddr, msgString)
				continue
			}

			receiveTimeNs := time.Now().UnixNano()
			endToEndLatencyMs := float64(receiveTimeNs-clientSendTimestampNs) / 1e6

			//Note: We don't know the *original* sender's address from this simple broadcast
			//unless the server explicitly adds it to the message

			//the sequence number is that of the original sender
			log.Printf("Recv (from Srv %s): Seq %d, Latency: %.3f ms, Msg: '%s'",
				remoteAddr, originalSenderSeqNum, endToEndLatencyMs, payload)

		} else{
			log.Printf("Received (malformed message structure from %s): %s", remoteAddr, msgString)
		}
	}
}

//sendMessages reads user input and sends to server
func sendMessages(conn *net.UDPConn, serverAddr *net.UDPAddr) {
	scanner := bufio.NewScanner(os.Stdin)
	log.Print("> ") //prompt for input

	for scanner.Scan() {
		userInput := scanner.Text()
		if userInput == "" {
			log.Print("> ")
			continue
		}

		currentSeqNum := atomic.AddUint64(&clientSequenceNumber, 1)
		sendTimestampNs := time.Now().UnixNano()

		//format: sequence_number|client_send_timestamp_ns|payload
		payload := fmt.Sprintf("%d|%d|%s", currentSeqNum, sendTimestampNs, userInput)

		_, err := conn.WriteToUDP([]byte(payload), serverAddr)

		if err != nil{

			log.Printf("Send error: %v", err)
			//UDP sends are often "fire and forget", errors here are less common
			//than with TCP, but can happen (e.g. network unreachable locally).
		}
		log.Printf("Sent (Seq %d): %s", currentSeqNum, userInput) //log what was sent
		log.Print("> ")
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading stdin: %v", err)
	}
	log.Println("Stdin closed, client exiting send loop.")
}
