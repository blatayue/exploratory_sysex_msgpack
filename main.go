package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/MarinX/keylogger"
	"github.com/gorilla/websocket"
	"github.com/vmihailenco/msgpack/v5"

	"os/signal"
    "syscall"
)

const (
	KNOB_LEFT  = -1
	KNOB_RIGHT = 1
)
	// a place for some bytes to get to the websocket
var eventChannel = make(chan []byte)


var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// https://github.com/bishopdynamics/superbird-debian-kiosk/blob/487bf52be79d366a055d880557607129ac92d694/files/data/scripts/buttons_app.py#L42
var keycodeMap = map[uint16]string{
	2:  "1",
	3:  "2",
	4:  "3",
	5:  "4",
	50: "m",
	28: "ENTER",
	1:  "ESC",
}

// send ws msg on keypress events (crude, but works as a proof of concept)
func handleEvents(device string) {
	file, err := os.Open(device)
	if err != nil {
		log.Fatalf("Failed to open device: %s, error: %v", device, err)
	}
	defer file.Close()

	var event keylogger.InputEvent

	for {
		// try read event
		err := binary.Read(file, binary.LittleEndian, &event)
		if err != nil {
			log.Fatalf("Failed to read input event: %v", err)
		}

		if device == "/dev/input/event0" { // button vals
			if event.Type == 1 { // EV_KEY
				if key, found := keycodeMap[event.Code]; found {
					eventChannel <- []byte(key + fmt.Sprintf(" %d", event.Value))
				}
			}
		} else if device == "/dev/input/event1" { // knob vals
			if event.Type == 2 { // EV_REL
				if event.Code == 6 { // REL_X
					if event.Value == KNOB_LEFT {
						eventChannel <- []byte("KNOB_LEFT")
					} else if event.Value == KNOB_RIGHT {
						eventChannel <- []byte("KNOB_RIGHT")
					}
				}
			}
		}
	}
}

// typical midi message
type midiMessage struct {
	Status uint8
	Data1  uint8
	Data2  uint8
}

// marshall a []uint8 to an array instead of a b64 string
type JSONableSlice []uint8

func (u JSONableSlice) MarshalJSON() ([]byte, error) {
	var result string
	if u == nil {
		result = "null"
	} else {
		result = strings.Join(strings.Fields(fmt.Sprintf("%d", u)), ",")
	}
	return []byte(result), nil
}

// listen for midi
func handleMIDIEvents() {
	device := "/dev/snd/midiC1D0"
	file, err := os.Open(device)
	if err != nil {
		log.Fatalf("Failed to open device: %s, error: %v", device, err)
	}
	defer file.Close()

	var event midiMessage

	for {
		err := binary.Read(file, binary.LittleEndian, &event)
		if err != nil {
			log.Fatalf("Failed to read input event: %v", err)
		}
		if event.Status == 0xF0 { // is Sysex
			// combine into uint16
			sysex_type := uint16(event.Data2)<<8 | uint16(event.Data1)

			// should be
			sysex_length_dbl_enc := make([]byte, 4) // uint16 doubled to clamp bytes under 127
			err := binary.Read(file, binary.LittleEndian, &sysex_length_dbl_enc)
			if err != nil {
				log.Fatalf("Failed to read sysex message length: %v", err)
			}

			// technically a uint16, 2 bytes
			var sysex_length_buf []byte
			for i := 0; i < len(sysex_length_dbl_enc)-1; i += 2 { // loops twice to make a uint16 from 4 uint8
				decoded_byte := (sysex_length_dbl_enc[i] << 1) + sysex_length_dbl_enc[i+1]
				sysex_length_buf = append(sysex_length_buf, decoded_byte)
			}

			// no length check, this seems safe
			sysex_length := uint16(sysex_length_buf[1])<<8 | uint16(sysex_length_buf[0])
			sysex_message := make([]byte, sysex_length) // yolo it
			err_sysex_msg := binary.Read(file, binary.LittleEndian, &sysex_message)
			if err_sysex_msg != nil {
				log.Fatalf("Failed to read sysex msg input: %v", err_sysex_msg)
			}

			// decode sysex from super inefficient clamped encoding to msgpack
			var msgpack_sysex []byte
			for i := 0; i < len(sysex_message); i += 2 {
				decoded_byte := (sysex_message[i] << 1) + sysex_message[i+1]
				msgpack_sysex = append(msgpack_sysex, decoded_byte)
			}
			if sysex_type == 0x01 {
				// decode msgpack to json
				var result interface{}
				err_msgpack := msgpack.Unmarshal(msgpack_sysex, &result)
				if err_msgpack != nil {
					log.Fatalf("Failed to read msgpack input: %v", err_msgpack)
				}

				// marshall to json
				result_json, err_json := json.Marshal(result)
				if err_json != nil {
					log.Fatalf("Failed to write json: %v", err_json)
				}
				// send over websocket
				eventChannel <- []byte(result_json)

			}
			// sysex end byte check, formality really
			var sysex_end uint8
			err_sysex_end := binary.Read(file, binary.LittleEndian, &sysex_end)
			if err_sysex_end != nil {
				log.Fatalf("Failed to read sysex end byte: %v", err_sysex_end)
			}
			if sysex_end == 0xF7 {
				continue
			}
		} else {
			type midiMessageT struct {
				Midi JSONableSlice `json:"midi"`
			}
			var midiMessage midiMessageT
			midiMessage.Midi = []uint8{event.Status, event.Data1, event.Data2}
			result_json, err_json := json.Marshal(midiMessage)
			if err_json != nil {
				log.Fatalf("Failed to write json: %v", err_json)
			}
			eventChannel <- result_json
			continue
		}
	}
}

var clients = make(map[*websocket.Conn]bool)

// does websocket stuff
func websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Error while upgrading connection:", err)
		return
	}
	defer conn.Close()

	clients[conn] = true
	fmt.Println("Client connected")

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("Error while reading message:", err)
			delete(clients, conn)
			break
		}
	}

}

func handleMessageWrites() {
	for {
		event := <-eventChannel
		for client := range clients {
			err := client.WriteMessage(websocket.TextMessage, []byte(event))
			if err != nil {
				fmt.Println("Error while sending message:", err)
				client.Close()
				delete(clients, client)
			}
		}
	}
}

func main() {
	// handle stop signals to be able to kill server with ctrl+c rather than "kill -9 $(jobs -p)"
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan bool, 1)
	go func() {
        sig := <-sigs
        fmt.Println()
        fmt.Println(sig)
		os.Exit(0)
        done <- true
    }()
	// end of signal stuff

	// listen to hw input events
	go handleEvents("/dev/input/event0")
	go handleEvents("/dev/input/event1")

	// listen to midi input events
	go handleMIDIEvents()

	// more websocket stuff
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		websocketHandler(w, r)
	})
	go handleMessageWrites()
	// It's nice to know the server is started
	fmt.Println("Starting server at :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))

}
