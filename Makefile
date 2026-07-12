EMBED_FRONTEND ?= 0
OUTPUT ?= build/chatapi

.PHONY: build frontend-embedded clean

ifeq ($(EMBED_FRONTEND),1)
build: frontend-embedded
	mkdir -p "$(dir $(OUTPUT))"
	cd backend && go build -buildvcs=false -tags embed_frontend -o "../$(OUTPUT)" ./cmd/server
else
build:
	mkdir -p "$(dir $(OUTPUT))"
	cd backend && go build -buildvcs=false -o "../$(OUTPUT)" ./cmd/server
endif

frontend-embedded:
	npm --prefix frontend run build:embedded

clean:
	rm -rf build backend/internal/http/webassets/dist
