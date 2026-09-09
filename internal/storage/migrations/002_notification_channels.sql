-- SPDX-License-Identifier: AGPL-3.0-or-later
CREATE TABLE notification_channel_state (
  channel TEXT PRIMARY KEY,
  suspended_until TEXT,
  suspension_reported_at TEXT
);
