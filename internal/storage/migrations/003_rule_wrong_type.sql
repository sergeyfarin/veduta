-- SPDX-License-Identifier: AGPL-3.0-or-later
ALTER TABLE rule_state ADD COLUMN wrong_type INTEGER NOT NULL DEFAULT 0;
