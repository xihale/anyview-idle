# anyview-idle —— Go 单二进制 + 内嵌 Vue (Element Plus) 前端
#
#   make web    前端构建 (bun install + vite build → web/dist)
#   make build  前端 + go build (产物 anyview-idle)
#   make test   go test ./...
#   make dev    前端热更开发 (vite :5173, /api 代理 :8765; 后端另起 go run . -addr 127.0.0.1:8765)
#   make clean

FRONTEND := web-src
OUT := anyview-idle

.PHONY: web build test dev clean

web:
	cd $(FRONTEND) && bun install --frozen-lockfile 2>/dev/null || cd $(FRONTEND) && bun install
	cd $(FRONTEND) && bun run build

build: web
	go build -o $(OUT) .

test:
	go test ./...

dev:
	cd $(FRONTEND) && bun run dev

clean:
	rm -f $(OUT)
	rm -rf $(FRONTEND)/node_modules web/dist
