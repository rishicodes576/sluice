# frozen_string_literal: true

require "json"
require "net/http"
require "uri"

module Sluice
  # Raised for non-2xx responses from the gateway.
  class Error < StandardError
    attr_reader :status, :type

    def initialize(message, status:, type: nil)
      super(message)
      @status = status
      @type = type
    end
  end

  # Result of a chat completion, augmented with Sluice gateway metadata.
  class ChatResult
    attr_reader :body, :cached, :provider

    def initialize(body, cached:, provider:)
      @body = body
      @cached = cached
      @provider = provider
    end

    # Convenience accessor for the assistant's message content.
    def content
      body.dig("choices", 0, "message", "content")
    end

    def usage
      body["usage"]
    end
  end

  # Client is a small, dependency-free client for the Sluice gateway's
  # OpenAI-compatible API, built on Ruby's standard library.
  class Client
    def initialize(base_url: "http://localhost:8080", api_key: nil, open_timeout: 5, read_timeout: 120)
      @base_url = base_url.sub(%r{/\z}, "")
      @api_key = api_key
      @open_timeout = open_timeout
      @read_timeout = read_timeout
    end

    # Perform a non-streaming chat completion. Returns a ChatResult.
    def chat(model:, messages:, **params)
      payload = { model: model, messages: messages }.merge(params)
      res = post("/v1/chat/completions", payload)
      body = JSON.parse(res.body)
      raise_for_status(res, body)
      ChatResult.new(body,
                     cached: res["x-sluice-cache"] == "hit",
                     provider: res["x-sluice-provider"] || model)
    end

    # Stream a chat completion, yielding content deltas to the block.
    def stream(model:, messages:, **params)
      payload = { model: model, messages: messages, stream: true }.merge(params)
      uri = URI("#{@base_url}/v1/chat/completions")
      req = build_request(uri, payload)

      http(uri).request(req) do |res|
        raise_for_status(res, safe_parse(res)) unless res.code.to_i < 300
        buffer = +""
        res.read_body do |segment|
          buffer << segment
          while (idx = buffer.index("\n"))
            line = buffer.slice!(0..idx).strip
            next unless line.start_with?("data:")

            data = line.sub(/\Adata:\s*/, "")
            return if data == "[DONE]"

            delta = JSON.parse(data).dig("choices", 0, "delta", "content") rescue nil
            yield delta if delta && block_given?
          end
        end
      end
    end

    # Create embeddings for a string or array of strings.
    def embeddings(model:, input:)
      res = post("/v1/embeddings", { model: model, input: input })
      body = JSON.parse(res.body)
      raise_for_status(res, body)
      body
    end

    # List available models.
    def models
      uri = URI("#{@base_url}/v1/models")
      req = Net::HTTP::Get.new(uri)
      req["authorization"] = "Bearer #{@api_key}" if @api_key
      res = http(uri).request(req)
      JSON.parse(res.body).fetch("data", [])
    end

    private

    def post(path, payload)
      uri = URI("#{@base_url}#{path}")
      http(uri).request(build_request(uri, payload))
    end

    def build_request(uri, payload)
      req = Net::HTTP::Post.new(uri)
      req["content-type"] = "application/json"
      req["authorization"] = "Bearer #{@api_key}" if @api_key
      req.body = JSON.generate(payload)
      req
    end

    def http(uri)
      h = Net::HTTP.new(uri.host, uri.port)
      h.use_ssl = uri.scheme == "https"
      h.open_timeout = @open_timeout
      h.read_timeout = @read_timeout
      h
    end

    def safe_parse(res)
      JSON.parse(res.body)
    rescue StandardError
      {}
    end

    def raise_for_status(res, body)
      code = res.code.to_i
      return if code < 300

      err = body.is_a?(Hash) ? body["error"] : nil
      raise Error.new(err&.fetch("message", "request failed") || "request failed (#{code})",
                      status: code, type: err && err["type"])
    end
  end
end
