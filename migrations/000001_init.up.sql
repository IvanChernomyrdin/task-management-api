CREATE TABLE
    users (
        id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
        email VARCHAR(254) NOT NULL UNIQUE,
        password_hash VARCHAR(255) NOT NULL,
        name VARCHAR(120) NOT NULL,
        created_at DATETIME (6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
    ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE
    teams (
        id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
        name VARCHAR(150) NOT NULL,
        created_by BIGINT UNSIGNED NOT NULL,
        created_at DATETIME (6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
        task_revision BIGINT UNSIGNED NOT NULL DEFAULT 0,
        CONSTRAINT fk_teams_creator FOREIGN KEY (created_by) REFERENCES users (id)
    ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE
    team_members (
        team_id BIGINT UNSIGNED NOT NULL,
        user_id BIGINT UNSIGNED NOT NULL,
        role ENUM ('owner', 'admin', 'member') NOT NULL,
        PRIMARY KEY (team_id, user_id),
        INDEX idx_team_members_user (user_id, team_id),
        CONSTRAINT fk_team_members_team FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE CASCADE,
        CONSTRAINT fk_team_members_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
    ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE
    tasks (
        id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
        team_id BIGINT UNSIGNED NOT NULL,
        title VARCHAR(200) NOT NULL,
        description TEXT NOT NULL,
        status ENUM ('todo', 'in_progress', 'done') NOT NULL DEFAULT 'todo',
        created_by BIGINT UNSIGNED NOT NULL,
        assignee_id BIGINT UNSIGNED NULL,
        created_at DATETIME (6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
        updated_at DATETIME (6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
        closed_at DATETIME (6) NULL,
        version BIGINT UNSIGNED NOT NULL DEFAULT 1,
        INDEX idx_tasks_team_id (team_id, id),
        INDEX idx_tasks_team_status_id (team_id, status, id),
        INDEX idx_tasks_team_assignee_id (team_id, assignee_id, id),
        INDEX idx_tasks_team_status_assignee_id (team_id, status, assignee_id, id),
        INDEX idx_tasks_team_closed (team_id, status, closed_at, assignee_id),
        CONSTRAINT fk_tasks_team FOREIGN KEY (team_id) REFERENCES teams (id),
        CONSTRAINT fk_tasks_creator FOREIGN KEY (created_by) REFERENCES users (id),
        CONSTRAINT fk_tasks_assignee FOREIGN KEY (assignee_id) REFERENCES users (id)
    ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE
    task_history (
        id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
        task_id BIGINT UNSIGNED NOT NULL,
        changed_by BIGINT UNSIGNED NOT NULL,
        changes JSON NOT NULL,
        created_at DATETIME (6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
        INDEX idx_task_history_task_id (task_id, id),
        CONSTRAINT fk_history_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
        CONSTRAINT fk_history_actor FOREIGN KEY (changed_by) REFERENCES users (id)
    ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE
    task_comments (
        id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
        task_id BIGINT UNSIGNED NOT NULL,
        user_id BIGINT UNSIGNED NOT NULL,
        content TEXT NOT NULL,
        created_at DATETIME (6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
        INDEX idx_comments_task_id (task_id, id),
        CONSTRAINT fk_comments_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
        CONSTRAINT fk_comments_user FOREIGN KEY (user_id) REFERENCES users (id)
    ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;