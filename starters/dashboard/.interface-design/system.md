# Dashboard design system

Use `docs/design-guide.md` for the complete rules and `src/lib/styles/tokens.css` for executable values.

Preserve Tarn's actual UI. The sidebar behavior and style come directly from its source. The audience is an operator finding stalled work and inspecting resources. Product names and models may change; the shell's polish should travel intact.

Use black and graphite in dark mode, Tarn's cool gray canvas and white stage in light mode, restrained green actions, semantic amber/rose/blue, Geist UI text, and Geist Mono data.

The stage has an 8px outer inset and 14px radius. Rack sections use a 12px radius and 16px by 18px padding. Controls and badges use 8px, navigation pills 5px, and the header toggle 6px. Use flat sections for overview content. Do not replace the inset stage with a full-bleed generic dashboard.

Spacing follows a 4px base. Stage padding is 24px horizontally and 20px vertically, or 16px on phones. The header title is 15px, section titles 13px, and metric values 20px.

The sidebar defaults to 320px, resizes between 180px and 520px, and collapses to a 56px rail. Center icons in 40px by 52px targets and mark the current route with a 16px by 3px underline pill. Reveal section descriptions and information rows at 296px and wider, inspired by the Goblin sidebar. Preserve the pointer-following edge handle, width hint, drag-to-collapse threshold, keyboard resize, and moving active/hover pills. At 640px and below navigation opens inline.

Settings use Tarn’s sticky scrolling submenu with a moving active pill and rounded sections. The Section template is a static generic recommendation, with structural placeholders and no invented fields, actions or loading animation.

Recurring patterns include the compact page header, divided metric strip, dotted topology canvas with expanded mode, quiet resource table, rounded detail section, and activity feed. Keep API behavior and resource nouns outside generic components.

Provide visible keyboard focus, pressed and disabled feedback, labeled inputs, text with status color, loading/empty/error states, and reduced-motion support. Label fixtures as sample data.
