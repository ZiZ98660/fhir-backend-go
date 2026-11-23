package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

const (
	mllpStartBlock = 0x0B
	mllpEndBlock1  = 0x1C
	mllpEndBlock2  = 0x0D
)

// MLLPServer accepts raw TCP connections carrying HL7 v2 messages framed with MLLP.
type MLLPServer struct {
	addr      string
	service   *Service
	scope     Scope
	listener  net.Listener
	quit      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// NewMLLPServer creates a new MLLP TCP listener.
func NewMLLPServer(addr string, service *Service, scope Scope) *MLLPServer {
	if addr == "" {
		addr = ":2575"
	}
	return &MLLPServer{
		addr:    addr,
		service: service,
		scope:   scope,
		quit:    make(chan struct{}),
	}
}

// Start begins listening for incoming TCP connections.
func (s *MLLPServer) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to start MLLP listener on %s: %w", s.addr, err)
	}
	s.listener = ln

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-s.quit:
					return
				default:
					log.Printf("[mllp] accept error: %v", err)
					continue
				}
			}
			s.wg.Add(1)
			go func(c net.Conn) {
				defer s.wg.Done()
				s.handleConn(c)
			}(conn)
		}
	}()
	return nil
}

// Close stops the MLLP listener and waits for active connections to finish.
func (s *MLLPServer) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.quit)
		if s.listener != nil {
			err = s.listener.Close()
		}
	})
	s.wg.Wait()
	return err
}

func (s *MLLPServer) handleConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	reader := bufio.NewReader(conn)
	for {
		// Read until start block (0x0B)
		b, err := reader.ReadByte()
		if err != nil {
			if err != io.EOF {
				log.Printf("[mllp] read start block error: %v", err)
			}
			return
		}
		if b != mllpStartBlock {
			continue // Scan forward until MLLP start byte
		}

		// Read payload until end block (0x1C 0x0D)
		var payload bytes.Buffer
		for {
			ch, err := reader.ReadByte()
			if err != nil {
				return
			}
			if ch == mllpEndBlock1 {
				next, err := reader.ReadByte()
				if err == nil && next == mllpEndBlock2 {
					break // End of MLLP frame reached
				}
				payload.WriteByte(ch)
				if err == nil {
					payload.WriteByte(next)
				}
				continue
			}
			payload.WriteByte(ch)
		}

		hl7Text := payload.String()
		result, err := ParseHL7Result(hl7Text)
		msgID := result.MessageID
		if msgID == "" {
			msgID = "UNKNOWN"
		}

		nowStr := time.Now().UTC().Format("20060102150405")
		if err != nil {
			log.Printf("[mllp] failed to parse HL7 from %s: %v", conn.RemoteAddr(), err)
			ack := fmt.Sprintf("MSH|^~\\&|CORE_EMR|MAIN_CAMPUS|LAB_DEVICE|HOSP_LAB|%s||ACK^R01|%s|P|2.5.1\rMSA|AE|%s|%s\r",
				nowStr, msgID, msgID, err.Error())
			_ = s.writeFrame(conn, ack)
			return
		}

		rawJSON, err := json.Marshal(result)
		if err != nil {
			log.Printf("[mllp] marshal error: %v", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		receipt, _, submitErr := s.service.Submit(ctx, s.scope, rawJSON)
		cancel()

		if submitErr != nil {
			log.Printf("[mllp] intake submission error for %s: %v", msgID, submitErr)
			ack := fmt.Sprintf("MSH|^~\\&|CORE_EMR|MAIN_CAMPUS|LAB_DEVICE|HOSP_LAB|%s||ACK^R01|%s|P|2.5.1\rMSA|AE|%s|%s\r",
				nowStr, msgID, msgID, submitErr.Error())
			_ = s.writeFrame(conn, ack)
			return
		}

		log.Printf("[mllp] accepted message %s (receipt: %s, state: %s) from %s", msgID, receipt.ID, receipt.State, conn.RemoteAddr())
		ack := fmt.Sprintf("MSH|^~\\&|CORE_EMR|MAIN_CAMPUS|LAB_DEVICE|HOSP_LAB|%s||ACK^R01|%s|P|2.5.1\rMSA|AA|%s|Message accepted\r",
			nowStr, msgID, msgID)
		_ = s.writeFrame(conn, ack)
	}
}

func (s *MLLPServer) writeFrame(conn net.Conn, text string) error {
	var buf bytes.Buffer
	buf.WriteByte(mllpStartBlock)
	buf.WriteString(text)
	buf.WriteByte(mllpEndBlock1)
	buf.WriteByte(mllpEndBlock2)
	_, err := conn.Write(buf.Bytes())
	return err
}
