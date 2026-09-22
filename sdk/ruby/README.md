# sluice-client (Ruby)

Ruby client for the [Sluice](../../README.md) LLM inference gateway.
Dependency-free — built on Ruby's standard library.

```ruby
require "sluice"

client = Sluice::Client.new(base_url: "http://localhost:8080", api_key: ENV["SLUICE_API_KEY"])

# Non-streaming
result = client.chat(
  model: "gpt-4o-mini",
  messages: [{ role: "user", content: "Explain semantic caching briefly." }]
)
puts result.content
puts "cached? #{result.cached} via #{result.provider}"

# Streaming
client.stream(model: "gpt-4o-mini", messages: [{ role: "user", content: "Haiku about Go." }]) do |delta|
  print delta
end

# Embeddings
client.embeddings(model: "mock-1", input: ["hello", "world"])
```

## Development

```bash
bundle install
bundle exec rspec
```
