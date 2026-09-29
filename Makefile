.PHONY: build test eval demo evals-gen

build:
	go build -o bin/agentops ./cmd/agentops

test:
	go vet ./...
	go test ./...

eval: build
	./bin/agentops eval --dir evals/cases

# Regenerate eval fixtures from scripts/gen_evals.py (needs PyYAML).
evals-gen:
	python3 scripts/gen_evals.py

# Run one eval case end to end and print the PR comment.
demo: build
	cd evals/cases/02-permset-missing-field && \
	  python3 -c 'import yaml,json;print(json.dumps(yaml.safe_load(open("case.yaml"))["changes"]))' > /tmp/changes.json && \
	  ../../../bin/agentops analyze --repo . --changes /tmp/changes.json --config ../../../agentops.yaml
