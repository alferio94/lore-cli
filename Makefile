PROFILE ?= .github/release-profiles/development-all-off.json
OUTPUT ?=

.PHONY: release-profile-ldflags build-profile
release-profile-ldflags:
	@./scripts/release-profile-ldflags.sh "$(PROFILE)"

build-profile:
	@test -n "$(OUTPUT)" || { echo "OUTPUT is required" >&2; exit 2; }
	@ldflags="$$(./scripts/release-profile-ldflags.sh "$(PROFILE)")"; \
		test -n "$$ldflags"; \
		go build -trimpath -ldflags "$$ldflags" -o "$(OUTPUT)" ./cmd/lore
