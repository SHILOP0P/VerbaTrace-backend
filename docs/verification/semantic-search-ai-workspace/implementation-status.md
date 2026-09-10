# Implementation status — 2026-09-09

IN PROGRESS. Do not claim release readiness.

Authoritative requirements: conversation + docs/specs/semantic-search-and-ai-workspace.md, amended by personal/company scopes; Start retrieval-only; paid embeddings with durable deduplication; one active department per company; allocations cannot exceed parent; fresh transfer allowance funded by destination; all wallet charges credit-only cards; version-aware reusable chunks; full-set research independent of narrative size; export without report editor.

Implemented and targeted-tested this session:
- DB active membership uniqueness + normalized company trigger, typed 409 for add/reactivation; concurrent adds only one winner.
- Invitation preserves company manager role.
- Personal chat scope and separate nullable company idempotency; Start plan assistant flags false.
- Multiple generated messages allowed; request hashes include filter sets and scope.
- Current document revision/content hash/profile/privacy checks; historical replies hidden if any manifest source disappeared.
- Bounded long-segment chunking and provider duplicate/zero vector validation.
- Adaptive narrative budget; overlong text rejected instead of sliced.
- Personal scope integration test verifies plan access, two replies, duplicate request, changed filter rejection, ownership and vanished source hiding.
- Frontend segmented scope and independent drafts; reuse network retry key; app theme variables. Build passed before latest edits.

LATEST USER UI amendments (all pending final visual acceptance): styled dropdowns/tooltips; delete chats; task-specific titles; collapsible right inspector; one-line input height matches send then auto grows; centered composer caption; universal icon without search/AI text; deliberate launcher and expanded overlay placement; smooth cheap scope slider in header/context row; +/@ classified context picker with icons (calls, folders, chats, period).

New server delete route and first-question title implemented, tests pending. Added CallIDs filters API/service/hash for upcoming context picker.

User Go-interview empty-result issue: read-only current DB check showed 46 calls, 46 transcribed, ZERO search documents. Previous reply was a misleading empty-index fallback. IndexNext now prepares LOCAL lexical chunks without paid calls; worker starts without provider key. Empty reply distinguishes zero calls / zero index / zero matches. Search-intent requests return found call titles without LLM. These latest changes compile + assistant integration tests pass; local API container not rebuilt yet, lexical worker not deployed yet. Need real search validation and handle Russian Go alias, title search, query extraction safely. No claim semantic quality verified.

Provider: openai/text-embedding-3-small approved; .env.example default set. Actual Monolit/.env has none of EMBEDDING_API_KEY/MODEL/ASSISTANT_API_KEY/MODEL; values never printed. Official OpenRouter page checked 2026-09-09: $0.02/M input tokens, 8192 max input. No paid live calls made.

Major remaining implementation: universal pricing/reserve/settlement/reconciliation for assistant+embeddings, budgets department/user+transfer, async durable runs, semantic paid index cache, full-set research/artifacts/drilldown/export, wallet all-operation details, explicit versions, chat context attachments, frontend rewrite and browser QA, acceptance matrix/golden quality.

Verification: targeted Go assistant tests + PostgreSQL integration passed. Department/invitation suites passed including concurrency targeted. First full gate formatting failed then formatter applied. Second full gate reached lint but sandbox context loading failed: no go files to analyze; rerun escalated rather than go mod tidy. Final full gate pending. Preserve unrelated mock_interviews/. No commit/push.
