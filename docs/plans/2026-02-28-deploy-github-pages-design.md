# Deploy Documentation Site to GitHub Pages

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Deploy the Docusaurus site from `website/` to https://hopbox.dev via GitHub Pages.

## Approach

1. **GitHub Actions workflow** — `deploy-docs.yml` triggers on push to `main` when `website/` changes. Installs bun, builds, deploys to GitHub Pages.
2. **CNAME file** — `website/static/CNAME` with `hopbox.dev` for custom domain.
3. **DNS (manual)** — A records to GitHub IPs, CNAME `www` → `hopboxdev.github.io`.
4. **Repo settings (manual)** — Enable Pages source: GitHub Actions. Enable HTTPS.
