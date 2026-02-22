"use client";

import { motion } from "framer-motion";
import { cn } from "@/lib/cn";
import { CodeBlock } from "./code-block";
import { SectionHeader } from "./section-header";

interface FeatureCard {
  title: string;
  description: string;
  icon: React.ReactNode;
  code: string;
  filename: string;
  colSpan?: number;
}

const features: FeatureCard[] = [
  {
    title: "Multi-Provider Routing",
    description:
      "Route to OpenAI, Anthropic, or any OpenAI-compatible API. Priority, cost-optimized, and round-robin strategies.",
    icon: (
      <svg
        className="size-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M16 3h5v5M4 20L21 3M21 16v5h-5M15 15l6 6M4 4l5 5" />
      </svg>
    ),
    code: `gw := nexus.New(
  nexus.WithProvider(openai.New(apiKey)),
  nexus.WithProvider(anthropic.New(apiKey)),
  nexus.WithRouter(strategies.CostOptimized()),
)`,
    filename: "main.go",
  },
  {
    title: "Content Guardrails",
    description:
      "PII detection, prompt injection, content filtering. Pre and post-processing guards with block, redact, and warn actions.",
    icon: (
      <svg
        className="size-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
      </svg>
    ),
    code: `nexus.WithGuard(guards.PII(guards.ActionRedact)),
nexus.WithGuard(guards.ContentFilter()),
nexus.WithGuard(guards.PromptInjection()),`,
    filename: "guards.go",
  },
  {
    title: "Multi-Tenant Isolation",
    description:
      "Every request is scoped to a tenant via context. API keys, rate limits, usage tracking, and model aliases per tenant.",
    icon: (
      <svg
        className="size-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2" />
        <circle cx="9" cy="7" r="4" />
        <path d="M23 21v-2a4 4 0 00-3-3.87M16 3.13a4 4 0 010 7.75" />
      </svg>
    ),
    code: `ctx = nexus.WithTenant(ctx, "tenant-1")

// All requests, usage, and rate limits
// scoped to tenant-1 automatically`,
    filename: "scope.go",
  },
  {
    title: "Pluggable Store Backends",
    description:
      "Start with in-memory for development, swap to SQLite or PostgreSQL for production. Every subsystem is a Go interface.",
    icon: (
      <svg
        className="size-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <ellipse cx="12" cy="5" rx="9" ry="3" />
        <path d="M21 12c0 1.66-4.03 3-9 3s-9-1.34-9-3" />
        <path d="M3 5v14c0 1.66 4.03 3 9 3s9-1.34 9-3V5" />
      </svg>
    ),
    code: `gw := nexus.New(
  nexus.WithDatabase(postgres.New(pool)),
  nexus.WithCache(stores.NewRedis(rdb)),
)
// Also: memory.New(), sqlite.New(db)`,
    filename: "main.go",
  },
  {
    title: "Model Aliases & Transforms",
    description:
      "Map virtual model names to provider targets. Apply input/output transforms, system prompt injection, and RAG context.",
    icon: (
      <svg
        className="size-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M7 7h10v10" />
        <path d="M7 17L17 7" />
        <rect x="2" y="2" width="20" height="20" rx="2" />
      </svg>
    ),
    code: `nexus.WithAlias("fast",
  model.AliasTarget{Provider: "anthropic",
    Model: "claude-3.5-haiku"},
)
// Clients request model: "fast"`,
    filename: "alias.go",
  },
  {
    title: "OpenAI-Compatible Proxy",
    description:
      "Drop-in replacement for the OpenAI API. Point any SDK at your nexus instance and get routing, caching, guardrails, and multi-tenancy for free.",
    icon: (
      <svg
        className="size-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M5 12h14M12 5l7 7-7 7" />
      </svg>
    ),
    code: `proxy := proxy.New(gw)
http.ListenAndServe(":8080", proxy)
// Any OpenAI SDK can connect to
// localhost:8080/v1/chat/completions`,
    filename: "proxy.go",
    colSpan: 2,
  },
];

const containerVariants = {
  hidden: {},
  visible: {
    transition: {
      staggerChildren: 0.08,
    },
  },
};

const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  visible: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.5, ease: "easeOut" as const },
  },
};

export function FeatureBento() {
  return (
    <section className="relative w-full py-20 sm:py-28">
      <div className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
        <SectionHeader
          badge="Features"
          title="Everything you need for AI gateway"
          description="Nexus handles the hard parts — routing, caching, guardrails, multi-tenancy, and model management — so you can focus on your application."
        />

        <motion.div
          variants={containerVariants}
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true, margin: "-50px" }}
          className="mt-14 grid grid-cols-1 md:grid-cols-2 gap-4"
        >
          {features.map((feature) => (
            <motion.div
              key={feature.title}
              variants={itemVariants}
              className={cn(
                "group relative rounded-xl border border-fd-border bg-fd-card/50 backdrop-blur-sm p-6 hover:border-violet-500/20 hover:bg-fd-card/80 transition-all duration-300",
                feature.colSpan === 2 && "md:col-span-2",
              )}
            >
              {/* Header */}
              <div className="flex items-start gap-3 mb-4">
                <div className="flex items-center justify-center size-9 rounded-lg bg-violet-500/10 text-violet-600 dark:text-violet-400 shrink-0">
                  {feature.icon}
                </div>
                <div>
                  <h3 className="text-sm font-semibold text-fd-foreground">
                    {feature.title}
                  </h3>
                  <p className="text-xs text-fd-muted-foreground mt-1 leading-relaxed">
                    {feature.description}
                  </p>
                </div>
              </div>

              {/* Code snippet */}
              <CodeBlock
                code={feature.code}
                filename={feature.filename}
                showLineNumbers={false}
                className="text-xs"
              />
            </motion.div>
          ))}
        </motion.div>
      </div>
    </section>
  );
}
