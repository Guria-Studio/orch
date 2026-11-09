---

description: "Task list for Photo albums"
---

# Tasks: Photo albums

**Input**: Design documents from `/specs/001-photo-albums/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: Included, as the spec asks for them.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

<!--
  ============================================================================
  IMPORTANT: The tasks below are SAMPLE TASKS for illustration purposes only.
  - [ ] T999 A sample task inside a comment, never imported
  ============================================================================
-->

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [x] T001 Create project structure per implementation plan
- [ ] T002 Initialize the Vite project with `package.json` dependencies
- [ ] T003 [P] Configure linting and formatting tools in `.oxlintrc.json`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

- [ ] T004 Setup database schema and migrations framework in src/db/schema.sql
- [ ] T005 [P] Implement storage adapter in `src/lib/storage.ts`
- [ ] T006 [P] Setup API routing in `src/api/router.ts`
- [ ] T006a Implement `Clock` and `IdSource` interfaces for tests in `src/lib/deps.ts`

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Create an album (Priority: P1) 🎯 MVP

**Goal**: Users can group photos into named albums

**Independent Test**: Create an album with two photos and reload: both are still in it.

### Tests for User Story 1 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [ ] T010 [P] [US1] Contract test for POST /albums in tests/contract/test_albums.py
- [ ] T011 [P] [US1] Integration test for album creation in tests/integration/test_create_album.py

### Implementation for User Story 1

- [ ] T012 [P] [US1] Create Album model in src/models/album.py
- [ ] T013 [P] [US1] Create Photo model in src/models/photo.py
- [ ] T014 [US1] Implement AlbumService in src/services/album_service.py (depends on T012, T013)
- [ ] T015 [US1] Implement POST /albums in src/api/albums.py
  with validation of the album name (1-80 characters)
- [X] T016 [US1] Add logging for album operations

**Checkpoint**: At this point, User Story 1 should be fully functional and testable independently

---

## Phase 4: Polish & Cross-Cutting Concerns

- [ ] T020 [P] Documentation updates in docs/albums.md
- [ ] TXXX Security hardening

---

## Parallel Example: User Story 1

```bash
# Launch all models for User Story 1 together:
- [ ] T012 [P] [US1] Create Album model in src/models/album.py
Task: "Create Photo model in src/models/photo.py"
```
