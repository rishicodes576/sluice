# frozen_string_literal: true

require_relative "lib/sluice/version"

Gem::Specification.new do |spec|
  spec.name          = "sluice-client"
  spec.version       = Sluice::VERSION
  spec.authors       = ["rishicodes576"]
  spec.summary       = "Ruby client for the Sluice LLM inference gateway (OpenAI-compatible)."
  spec.description   = "A small, dependency-free Ruby client for Sluice: chat, streaming and embeddings."
  spec.homepage      = "https://github.com/rishicodes576/sluice"
  spec.license       = "Apache-2.0"
  spec.required_ruby_version = ">= 3.0"

  spec.files         = Dir["lib/**/*.rb", "README.md"]
  spec.require_paths = ["lib"]

  spec.add_development_dependency "rspec", "~> 3.12"
  spec.metadata["rubygems_mfa_required"] = "true"
end
