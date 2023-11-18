build:
	go build -o bin/tutlor

export arch="x86_64"

run: build
	./bin/tutlor

static:
	CGO_ENABLED=0 GOOS=linux go build -a -tags netgo -ldflags '-extldflags "-static"'  -installsuffix cgo -o bin/agent .


# Build a root filesystem for alpine
root: build-root extract image
	
build-root:
	docker build -t tutlor/mysql-agent .

# Allocate a 500MB disk image, then extract the rootfs.tar from the 
# container into it
# sudo fallocate -l 1GB ./rootfs.img  
image:
	set -e 
	rm -rf rootfs.img || : ;\
	dd if=/dev/zero of=rootfs.img bs=1M count=1000  ;\
	sudo mkfs.ext4 ./rootfs.img  ;\
	TMP=$$(mktemp -d)  ;\
	echo $$TMP  ;\
	sudo mount -o loop ./rootfs.img $$TMP  ;\
	sudo tar -xvf rootfs.tar -C $$TMP  ;\
	sudo umount $$TMP

# Extract a root filesystem into a tar
extract: 
	docker rm -f extract || :
	rm -rf rootfs.tar || :
	docker run -i -h tutlor --name extract tutlor/mysql-agent sh <./setup-alpine.sh
	docker export extract -o rootfs.tar
	docker rm -f extract

# Get the AWS sample image
# change to Image when using aarch64, instead of vmlinux.bin
kernel:
	curl -o vmlinux -S -L "https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/$(arch)/kernels/vmlinux.bin"
	file ./vmlinux

firecracker:
	sudo rm -f /tmp/firecracker.socket || :
	sudo firecracker --api-sock /tmp/firecracker.socket

