# Msgpack over MIDI Sysex for Car Thing

<details open>
<summary><h2>Overview</h2></summary>

A golang server and a jupyter notebook (okay, maybe 2 notebooks)

Server runs on Car Thing device, Jupyter notebook runs on pc

Rough data path:  
encode json as msgpack -> encode custom TLV -> MIDI Sysex -> MIDI OUT -> MIDI IN -> identify Sysex -> decode TLV -> decode msgpack as json -> WS  

### Golang Server

Hosts Websocket Server that processes and sends the hw input events (buttons, knobs) and midi input to the car thing as ws messages

### Jupyter Notebooks

>#### midi_messages.ipynb

Contains test midi data to send to device input. Supports normal midi messages and custom sysex msgpack implementation

>#### websocket_client.ipynb

Tiny notebook with separate websocket client to not deal with threading/asyncio/different client like postman

</details>

<details>
    <summary><h2>Steps to Build and Run</h2></summary>

### 1. Prep to Cross-Build Golang Server:

1. [Install Go](https://go.dev/doc/install)
2. Clone/Download this repo and cd into it in a terninal 
3. Set these Environment Variables for Go cross-building:
   - GOARCH=arm
   - GOOS=linux
   - Temporarily set env vars from terminal:
     - Powershell: >`$env:GOARCH='arm'; $env:GOOS='linux'`
     - Bash: >`export GOARCH=arm && export GOOS=linux`

### 2. Get dependencies and build arm binary

> `go get`  
> `go build .\main.go`

### 3. Install python dependencies for jupyter notebooks

> `pip install -r requirements.txt` (global install by default)


### 4. Set system read/write, push gadget script and binary to device

> `adb shell mount -o remount,rw /`  
> `adb push ./main /home/superbird/midi_hw_ws_server`  
> `adb push ./S49usbgadget /etc/init.d/`

### 5. Allow execution for gadget script (IMPORTANT) and go binary

> `adb shell chmod +x /etc/init.d/S49usbgadget`  
> `adb shell chmod +x /home/superbird/midi_hw_ws_server`

### 6. Reboot Car Thing

> `adb shell reboot`

### 7. Start Golang Server

> `adb shell /home/superbird/midi_hw_ws_server`

### 8. Start WS Client

> `ws://192.168.7.2:8080/ws`

A ws client is provided in websocket_client.ipynb
Alternatively, Postman is also simple to setup to listen for messages

</details>
