package integration

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestMLLPHandleConnWithNetPipe(t *testing.T) {
	service := NewService(&captureStore{})
	scope := Scope{TenantID: "synthetic-demo", SourceID: "synthetic-lis"}
	server := NewMLLPServer("", service, scope)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	go server.handleConn(serverConn)

	sampleHL7 := "MSH|^~\\&|COBAS_C311|HOSP_LAB|CORE_EMR|MAIN_CAMPUS|20261123103000||ORU^R01|MSG-TCP-001|P|2.5.1\r" +
		"PID|1||00001^^^SYNTHETIC_LIS^MR||Smith^John^^^^^L||19850412|M\r" +
		"ORC|RE|SYN-ORDER-1^LIS|ORD-98765^EMR||CM\r" +
		"OBR|1|SYN-ORDER-1^LIS|ORD-98765^EMR|GLU^Glucose, Fasting^LN|||20261123102000\r" +
		"OBX|1|NM|SYN-GLU^Synthetic numeric glucose scenario^SYN_LAB|1|5.20|mmol/L^mmol/L^UCUM|3.9-6.1|N|||F|||20261123102000"

	var frame bytes.Buffer
	frame.WriteByte(mllpStartBlock)
	frame.WriteString(sampleHL7)
	frame.WriteByte(mllpEndBlock1)
	frame.WriteByte(mllpEndBlock2)

	go func() {
		_, _ = clientConn.Write(frame.Bytes())
	}()

	reply := make([]byte, 1024)
	n, err := clientConn.Read(reply)
	if err != nil {
		t.Fatalf("failed reading MLLP ACK: %v", err)
	}

	res := string(reply[:n])
	if !bytes.Contains(reply[:n], []byte("MSA|AA|MSG-TCP-001")) {
		t.Fatalf("expected positive ACK with MSG-TCP-001, got: %s", res)
	}
}

func TestMLLPServerLifecycle(t *testing.T) {
	service := NewService(&captureStore{})
	scope := Scope{TenantID: "synthetic-demo", SourceID: "synthetic-lis"}
	server := NewMLLPServer("127.0.0.1:0", service, scope)

	err := server.Start()
	if err != nil {
		t.Skipf("skipping live socket test in restricted environment: %v", err)
		return
	}
	defer server.Close()

	time.Sleep(10 * time.Millisecond)
	_ = server.Close()
}
