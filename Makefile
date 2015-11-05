GO 			:= GO15VENDOREXPERIMENT=1 go
GOGETTER	:= GOPATH=$(shell pwd)/.tmpdeps go get -d
MKDIR		:= mkdir
INSTALL		:= install

all:
	$(GO) build -o galilego .

go_vendor_dependencies:
	if [ ! -d .tmpdeps ]; then $(MKDIR) .tmpdeps; fi
	$(GOGETTER) github.com/gorilla/mux
	$(GOGETTER) github.com/bradfitz/http2
	$(GOGETTER) github.com/nfnt/resize
	echo 'removing .git from vendored pkg and moving them to vendor'
	find .tmpdeps/src -type d -name ".git" ! -name ".gitignore" -exec rm -rf {} \; || exit 0
	cp -ar .tmpdeps/src/* vendor/
	rm -rf .tmpdeps
