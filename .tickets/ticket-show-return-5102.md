---
id: ticket-show-return-5102
stage: triage
deps: []
links: []
created: 2026-03-12T17:17:36Z
type: bug
priority: 1
---
# ticket_show can return a lot of content on tickets with large notes fields

When a ticket has a large content space ticket_show can consume a lot of tokens. Perhaps we should implement paging (I think we recently did this for ticket_list. Or maybe ticket_show should by default only return ticket meta data and require a flag to get full ticket results.
