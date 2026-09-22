# frozen_string_literal: true

require "sluice"
require "webrick"

# Spins up a tiny local HTTP server so the client is tested against real
# Net::HTTP behaviour without any external dependency.
RSpec.describe Sluice::Client do
  around do |example|
    server = WEBrick::HTTPServer.new(Port: 0, Logger: WEBrick::Log.new(File::NULL), AccessLog: [])
    @port = server.config[:Port]

    server.mount_proc "/v1/chat/completions" do |req, res|
      body = JSON.parse(req.body)
      if body["stream"]
        res.content_type = "text/event-stream"
        res.body = "data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}\n\n" \
                   "data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n" \
                   "data: [DONE]\n\n"
      else
        res["x-sluice-cache"] = "hit"
        res["x-sluice-provider"] = "mock"
        res.content_type = "application/json"
        res.body = JSON.generate(
          "id" => "1", "object" => "chat.completion", "model" => body["model"],
          "choices" => [{ "index" => 0, "message" => { "role" => "assistant", "content" => "hi" }, "finish_reason" => "stop" }],
          "usage" => { "prompt_tokens" => 1, "completion_tokens" => 1, "total_tokens" => 2 }
        )
      end
    end

    Thread.new { server.start }
    example.run
    server.shutdown
  end

  subject(:client) { described_class.new(base_url: "http://127.0.0.1:#{@port}") }

  it "returns content and cache metadata" do
    result = client.chat(model: "mock-1", messages: [{ role: "user", content: "hey" }])
    expect(result.content).to eq("hi")
    expect(result.cached).to be(true)
    expect(result.provider).to eq("mock")
  end

  it "streams content deltas" do
    chunks = []
    client.stream(model: "mock-1", messages: [{ role: "user", content: "x" }]) { |d| chunks << d }
    expect(chunks.join).to eq("Hello world")
  end
end
