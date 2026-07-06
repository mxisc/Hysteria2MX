CREATE TABLE IF NOT EXISTS hysteria_subscription_identities (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    username VARCHAR(64) NOT NULL,
    public_id VARCHAR(64) NOT NULL,
    token_secret VARCHAR(64) NOT NULL,
    refreshed_at DATETIME NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uniq_subscription_username (username),
    UNIQUE KEY uniq_subscription_public_id (public_id)
);

INSERT INTO hysteria_subscription_identities (username, public_id, token_secret)
SELECT grouped.username,
       CONCAT('sub_', SUBSTRING(SHA2(CONCAT(grouped.username, ':', UUID()), 256), 1, 20)),
       SUBSTRING(SHA2(CONCAT(grouped.username, ':', UUID(), ':secret'), 256), 1, 32)
FROM (
    SELECT username
    FROM hysteria_users
    GROUP BY username
) AS grouped
LEFT JOIN hysteria_subscription_identities existing ON existing.username = grouped.username
WHERE existing.id IS NULL;
