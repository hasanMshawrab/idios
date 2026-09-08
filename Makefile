.PHONY: build test lint ascii hooks run clean smoke generate generate-check app app-test app-icon

build:
	mkdir -p bin
	go build -o bin/idios ./cmd/idios

test:
	go test ./...

lint:
	go vet ./...
	golangci-lint run ./...

ascii:
	hack/ascii-check

generate:
	buf generate

generate-check: generate
	git diff --exit-code -- internal/apigen api/openapi

hooks:
	install -m 0755 hack/pre-commit .git/hooks/pre-commit

# PORT exists because the installed application's daemon holds 7770.
run: build
	./bin/idios -data-dir .storage -kubeconfig ./kube/config $(if $(PORT),-listen 127.0.0.1:$(PORT)) run

clean:
	rm -rf bin

smoke: build
	SMOKE_LISTEN=$(if $(PORT),127.0.0.1:$(PORT)) hack/smoke/run.sh

app:
	xcodebuild -project macos/idios.xcodeproj -scheme idios -configuration Debug \
		-derivedDataPath macos/DerivedData -skipPackagePluginValidation -quiet build

app-test:
	swift test --package-path macos

app-icon:
	hack/macos/icon.py
