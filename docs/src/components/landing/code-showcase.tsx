"use client";

import { motion } from "framer-motion";
import { CodeBlock } from "./code-block";
import { SectionHeader } from "./section-header";

const setupCode = `package main

import (
  "context"
  "os"

  "github.com/xraph/nexus"
  "github.com/xraph/nexus/model"
  "github.com/xraph/nexus/providers/openai"
  "github.com/xraph/nexus/providers/anthropic"
  "github.com/xraph/nexus/store/memory"
)

func main() {
  gw := nexus.New(
    nexus.WithDatabase(memory.New()),
    nexus.WithProvider(openai.New(
      os.Getenv("OPENAI_KEY"))),
    nexus.WithProvider(anthropic.New(
      os.Getenv("ANTHROPIC_KEY"))),
    nexus.WithAlias("fast", model.AliasTarget{
      Provider: "anthropic",
      Model:    "claude-3.5-haiku",
    }),
  )

  gw.Initialize(context.Background())
}`;

const completeCode = `package main

import (
  "context"
  "fmt"

  "github.com/xraph/nexus"
  "github.com/xraph/nexus/provider"
)

func complete(
  engine *nexus.Engine,
  ctx context.Context,
) {
  ctx = nexus.WithTenant(ctx, "tenant-1")

  resp, _ := engine.Complete(ctx,
    &provider.CompletionRequest{
      Model: "fast", // resolves via alias
      Messages: []provider.Message{{
        Role:    "user",
        Content: "What is Go?",
      }},
    })

  fmt.Println(resp.Choices[0].Message.Content)
  // "Go is a compiled programming language..."
}`;

export function CodeShowcase() {
  return (
    <section className="relative w-full py-20 sm:py-28">
      <div className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
        <SectionHeader
          badge="Developer Experience"
          title="Simple API. Powerful gateway."
          description="Configure providers and route requests in under 20 lines. Nexus handles the rest."
        />

        <div className="mt-14 grid grid-cols-1 lg:grid-cols-2 gap-6">
          {/* Setup side */}
          <motion.div
            initial={{ opacity: 0, x: -20 }}
            whileInView={{ opacity: 1, x: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.5, delay: 0.1 }}
          >
            <div className="mb-3 flex items-center gap-2">
              <div className="size-2 rounded-full bg-violet-500" />
              <span className="text-xs font-medium text-fd-muted-foreground uppercase tracking-wider">
                Setup &amp; Configure
              </span>
            </div>
            <CodeBlock code={setupCode} filename="main.go" />
          </motion.div>

          {/* Complete side */}
          <motion.div
            initial={{ opacity: 0, x: 20 }}
            whileInView={{ opacity: 1, x: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.5, delay: 0.2 }}
          >
            <div className="mb-3 flex items-center gap-2">
              <div className="size-2 rounded-full bg-green-500" />
              <span className="text-xs font-medium text-fd-muted-foreground uppercase tracking-wider">
                Route &amp; Complete
              </span>
            </div>
            <CodeBlock code={completeCode} filename="complete.go" />
          </motion.div>
        </div>
      </div>
    </section>
  );
}
