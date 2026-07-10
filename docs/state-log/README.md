# state-log/ — append-only event history

One file per event. Files are **never modified or deleted** after creation, and
**nothing in the pipeline reads this directory wholesale** — it exists for human
traceability and audit. The current pipeline state always lives in
`../project-state.yaml`.


