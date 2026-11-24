-- Refuse rollback while an account or catalog entry still references this provider.
DELETE FROM providers WHERE code='valueclouds';
