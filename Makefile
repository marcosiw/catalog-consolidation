# Main entry point of the project (SPEC section 6). Each target only wraps
# the commands below; every variable can be overridden: make run THRESHOLD=0.7

DATA_URL   := https://engineering-hiring-process.s3.us-east-1.amazonaws.com
DB         ?= data/catalog.db
INPUT      ?= data/ProductEntry.json
THRESHOLD  ?= 0.6
GLOSSARY   ?= config/glossary-pt-en.json
EXCLUSIONS ?= config/exclusion-list.json
REPORT_DIR ?= reports
BATCH_SIZE ?= 100

CONSOLIDATE = go run ./cmd/consolidate \
	-db $(DB) \
	-input $(INPUT) \
	-similarity-threshold $(THRESHOLD) \
	-glossary $(GLOSSARY) \
	-exclusion-list $(EXCLUSIONS) \
	-report-dir $(REPORT_DIR) \
	-batch-size $(BATCH_SIZE)

.DEFAULT_GOAL := help
.PHONY: help data reset test run inspect

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  make %-9s %s\n", $$1, $$2}'

data: ## Download the provided database and seller file, and verify their checksums
	@mkdir -p data
	curl -sSfo data/catalog.db $(DATA_URL)/catalog.db
	curl -sSfo data/ProductEntry.json $(DATA_URL)/ProductEntry.json
	sha256sum -c config/checksums.sha256

reset: ## Download the database again, back to its original state (make run changes it)
	@mkdir -p data
	curl -sSfo data/catalog.db $(DATA_URL)/catalog.db
	grep 'data/catalog.db' config/checksums.sha256 | sha256sum -c -

test: ## Run go vet and every test; no network or downloaded data needed
	go vet ./...
	go test ./...

run: ## Import for real; running it twice shows idempotency
	$(CONSOLIDATE)

inspect: ## Show the result of the imports (needs the sqlite3 command)
	@command -v sqlite3 >/dev/null || { echo "sqlite3 not found: install it (e.g. sudo apt install sqlite3)"; exit 1; }
	@echo "== Import runs"
	@sqlite3 -header -column $(DB) "SELECT Id, Status, StartedAt, Created, Linked, Moved, Unchanged, Rejected, DuplicateInBatch FROM ImportRun ORDER BY Id"
	@echo; echo "== Links by match method"
	@sqlite3 -header -column $(DB) "SELECT MatchMethod, count(*) AS Links FROM SellerProduct GROUP BY MatchMethod"
	@echo; echo "== Links made by translation"
	@sqlite3 -header -column $(DB) "SELECT s.SellerName, s.SellerProductName, p.Id AS ProductId, p.Name AS CatalogName, round(s.MatchScore, 2) AS Score FROM SellerProduct s JOIN Product p ON p.Id = s.ProductId WHERE s.MatchMethod = 'translated'"
	@echo; echo "== Products with the most sellers"
	@sqlite3 -header -column $(DB) "SELECT p.Id, p.Name, count(DISTINCT s.SellerName) AS Sellers FROM Product p JOIN SellerProduct s ON s.ProductId = p.Id GROUP BY p.Id ORDER BY Sellers DESC, p.Id LIMIT 5"
	@echo; echo "== Sellers linking one product with more than one id"
	@sqlite3 -header -column $(DB) "SELECT SellerName, ProductId, group_concat(SellerProductId, ', ') AS SellerIds FROM SellerProduct GROUP BY SellerName, ProductId HAVING count(*) > 1"
