# Domain-specific handlers for command logic

Multiple commands now share common business logic
through domain-specific handlers in `internal/handler/`.
Each handler encapsulates the core functionality for a particular domain,
allowing commands to delegate complex operations
rather than duplicating logic.

Current handlers include:

- `track/`: Branch tracking and restacking verification
  (https://github.com/abhinav/git-spice/pull/739)
- `checkout/`: Branch checkout with automatic restack verification
  (https://github.com/abhinav/git-spice/pull/740)
- `submit/`: Change request submission across different command variants
  (https://github.com/abhinav/git-spice/pull/741)

This replaces the previous `spice.Service` abstraction approach,
which proved too broad and unwieldy.
The new pattern provides:

1. **Domain separation**: Each handler focuses on a specific concern
2. **Testability**: Handlers can be unit tested in isolation
3. **Reusability**: Multiple commands can share the same underlying logic
4. **Interface segregation**:
   Handlers define minimal interfaces for their dependencies

Commands instantiate handlers with their required dependencies
and delegate core operations to them,
keeping command-specific logic (CLI parsing, output formatting)
separate from business logic.
