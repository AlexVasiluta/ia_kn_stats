# Usage

```sh
# Just scrape infoarena into dump.db
go run . -scrape_forward=true -export_stats=false

# Export information from both kn and infoarena to an HTML file
go run . -export_path="./output.html" -kilonova_dsn="DSN FROM config.toml" # ...

go run . -help # prints help page with all flags
```
