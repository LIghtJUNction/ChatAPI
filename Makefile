EMBED_FRONTEND ?= 0
OUTPUT ?= build/chatapi
GO_TAGS ?=

ifeq ($(EMBED_FRONTEND),1)
override GO_TAGS += embed_frontend
endif

GO_TAGS_ARG = $(if $(strip $(GO_TAGS)),-tags '$(strip $(GO_TAGS))',)

.PHONY: build shared production check-shared-deps frontend-embedded clean

ifeq ($(EMBED_FRONTEND),1)
build: frontend-embedded
	mkdir -p "$(dir $(OUTPUT))"
	cd backend && go build -buildvcs=false $(GO_TAGS_ARG) -o "../$(OUTPUT)" ./cmd/server
else
build:
	mkdir -p "$(dir $(OUTPUT))"
	cd backend && go build -buildvcs=false $(GO_TAGS_ARG) -o "../$(OUTPUT)" ./cmd/server
endif

shared:
	$(MAKE) build GO_TAGS="$(GO_TAGS) shared_image_processor"

production:
	$(MAKE) build EMBED_FRONTEND=1 GO_TAGS="$(GO_TAGS) shared_image_processor"

check-shared-deps:
	@deps="$$(cd backend && go list -buildvcs=false -tags shared_image_processor -deps ./cmd/server)"; \
	if printf '%s\n' "$$deps" | grep -E 'github.com/gen2brain/avif|github.com/tetratelabs/wazero'; then \
		echo "shared build unexpectedly includes local AVIF dependencies" >&2; exit 1; \
	fi

frontend-embedded:
	npm --prefix frontend run build:embedded

clean:
	rm -rf build backend/internal/http/webassets/dist
