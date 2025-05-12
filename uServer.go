//Writer: Jeronimo Tzib
//Server Features:
//- accepts messages from clients
//- broadcasts messages to all connected clients
//- tracks client activity with timeout detection
//- thread-safe client management

package main

import(

	"log"
	"net"
	"sync"
	"time"
)

//client represents a connected client with last activity timestamp

type client struct{
	addr *net.UDPAddr //Network address of client
	lastActive time.Time //Last message received time for timeout detection
}

//server mantains shared state for the UDP chat server
type server struct{
	conn *net.UDPConn //UDP network connection
	clients map[string]*client //Map of connected clients
	clientsMutex sync.RWMutex //Mutex for concurrent access to clients map
}

//NewServer creates and initializes a new UDP server instance
func NewServer(addr string)(*server, error){

	//resolve UDP address from string
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil{
		return nil, err
	}

	//create udp listener
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil{
		return nil, err
	}

	return &server{
		conn: conn,
		clients: make(map[string]*client),
	}, nil
}

//Start begins the server's background processing routines
func (s *server) Start(){

	//handle incoming messages in a goroutine
	go s.handleMessages()

	//start client cleanup timer in another goroutine
	go s.cleanupInactiveClients()
}

//handleMessages continuously reads from UDP socket and processes messages

func (s *server) handleMessages(){

	buffer := make([]byte, 1024)//reusable buffer for incoming datagrams

	for{
		//Read from UDP connection (blocks until data is received)
		n, addr, err := s.conn.ReadFromUDP(buffer)
		if err != nil{
			log.Printf("Read error: %v", err)
			continue
		}

		//Update client activity and broadcast mesage
		msg := string(buffer[:n])
		s.updateClient(addr) //update or add client
		s.broadcast(msg, addr) // send to all other clients
	}


}

//updateCLient adds/updates a client in the connection pool
func (s *server) updateClient(addr *net.UDPAddr){
	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()

	key := addr.String()
	if _, exists := s.clients[key]; !exists{

		//New client connection
		log.Printf("New client: %s", key)
		s.clients[key] = &client{addr, time.Now()}
	} else {
		//update existing client's activity time
		s.clients[key].lastActive = time.Now()
	}
}

//broadcast sends a message to all connected clients except the sender
func (s *server) broadcast(msg string, exclude *net.UDPAddr){

	s.clientsMutex.RLock() //Read lock for concurrent access
	defer s.clientsMutex.RUnlock()

	for _, c := range s.clients {
		if c.addr.String() == exclude.String() {
			continue //Skip the original sender
		}

		//Send message concurrently to prevent blocking
		go func(addr *net.UDPAddr) {
			if _, err := s.conn.WriteToUDP([]byte(msg), addr); err != nil {
				log.Printf("Send error to %s: %v", addr, err)
			}
		}(c.addr)
	}

}

//cleanupInactiveClients periodically removes clients that haven't sent messages
func (s *server) cleanupInactiveClients(){
	ticker := time.NewTicker(10 * time.Second)// Check every 10 seconds
	defer ticker.Stop()

	for range ticker.C{
		s.clientsMutex.Lock()
		for k, c := range s.clients{
			// Remove clients inactive for more than 30 seconds
			if time.Since(c.lastActive) > 30*time.Second{
				log.Printf("Client %s timed out", k)
				delete(s.clients, k)
			}
		}
		s.clientsMutex.Unlock()
	}
}

//Server entry point
func main() {
	srv, err := NewServer(":8080")
	if err != nil {
		log.Fatal(err)
	}
	srv.Start()
	select {} //Block main goroutine indefinitely
}