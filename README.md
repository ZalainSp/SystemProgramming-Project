
# TCP vs UDP Chat App in Go

## Project Overview
This project implements a real-time chat application using both TCP and UDP protocols in Go, comparing their performance characteristics in a controlled environment. The system consists of separate server and client implementations for each protocol, with metrics collection and analysis capabilities.

## Team Members
- Zalain Spain
- Jeronimo Tzib

## Course Information
- Course: CMPS 2242
- Lecturer: Mr. Dalwin Lewis

## Project Goals
- Compare TCP and UDP in a real-time chat system
- Learn network protocol tradeoffs
- Apply hands-on concurrency patterns
- Prepare for real-world application development

## Project Deliverables
- [YouTube Video Demo](#) (https://youtu.be/rDf2_i4mqW8)
- [Google Slides Presentation](#) (https://docs.google.com/presentation/d/1tBv7Bb-GAy-8fA7bNQn9ShiAMtranu3nsKiRO1wBOAI/edit?usp=sharing)


## How to Run
### TCP Chat
1. Start server: `go run server.go -port 4000 -max 10`
2. Connect clients: `nc localhost 4000`

### UDP Chat
1. Start server: `go run uServer.go`
2. Connect clients: `go run uClient.go localhost:8080`

## Dependencies
- Go 1.18+
- Standard library packages:
  - `net`
  - `sync`
  - `time`
  - `bufio`
