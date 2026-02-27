# Landing Page Redesign

**Goal:** Redesign the hopbox.dev landing page from generic Docusaurus template to a minimal, Zed-inspired layout with the terminal demo as the hero visual.

**Inspiration:** zed.dev — bold headline, tight feature ribbon, one big product visual.

## Structure

### 1. Hero (above the fold)
- Bold headline with monospace `hop` for terminal personality
- One-line subtitle
- Two CTA buttons: Get Started + GitHub
- Terminal window directly below buttons — the centerpiece
- Terminal shows `hop setup` → `hop up` with colored output

### 2. Feature ribbon
- Single horizontal bar, subtle background tint
- 3 features in a row: WireGuard Tunnel, Workspace Manifest, Workspace Mobility
- Bold title + one-line description each. No icons, no cards.

### 3. Footer
- Existing Docusaurus footer. Page ends here.

## Style changes
- Tighter vertical padding (hero padding roughly halved)
- Terminal moves from page bottom into hero section
- Feature grid → horizontal ribbon with background color
- Dark terminal gets subtle box-shadow
- Kill dead whitespace between sections

## Files touched
- `website/src/pages/index.tsx`
- `website/src/pages/index.module.css`
