SRCS = main.go myprotcol.go packet.go udp.go
TARO = 169.254.155.219:8888
HANAKO = 169.254.229.153:8888
CMD = ./rbp

runA: rbp
	$(CMD) -src=$(TARO) -dst=$(HANAKO) -mode=0

runB: rbp
	$(CMD) -src=$(HANAKO) -dst=$(TARO) -mode=1

runC: rbp
	$(CMD) -src=$(TARO) -dst=$(HANAKO) -mode=1

runD: rbp
	$(CMD) -src=$(TARO) -dst=$(HANAKO) -mode=0

rbp: $(SRCS)
	go build -o rbp $(SRCS)

clean:
	rm -f data/*
	rm -f rbp