-- 口述历史采集工具数据库初始化脚本
-- 由 MySQL 官方镜像 docker-entrypoint-initdb.d 在首次启动时执行
CREATE DATABASE IF NOT EXISTS oralhistory_db DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- 创建应用账号（若镜像环境变量已创建则忽略）
CREATE USER IF NOT EXISTS 'oralhistory_user'@'%' IDENTIFIED BY 'oralhistory_pwd';
GRANT ALL PRIVILEGES ON oralhistory_db.* TO 'oralhistory_user'@'%';
FLUSH PRIVILEGES;

-- 转写校对模块：转写稿版本表（同一录音可保留多个版本，仅最新版本可编辑）
CREATE TABLE IF NOT EXISTS transcripts (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  recording_id BIGINT UNSIGNED NOT NULL,
  project_id BIGINT UNSIGNED NOT NULL,
  version INT NOT NULL DEFAULT 1,
  status VARCHAR(32) NOT NULL DEFAULT 'draft',
  review_comment VARCHAR(512) DEFAULT '',
  created_by BIGINT UNSIGNED NOT NULL,
  submitted_by BIGINT UNSIGNED NOT NULL DEFAULT 0,
  submitted_at DATETIME(3) NULL,
  reviewed_by BIGINT UNSIGNED NOT NULL DEFAULT 0,
  reviewed_at DATETIME(3) NULL,
  created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_transcripts_recording_version (recording_id, version),
  INDEX idx_transcripts_project (project_id),
  INDEX idx_transcripts_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 转写校对模块：转写分段表（按时间轴记录说话人与内容，档案员逐段确认）
CREATE TABLE IF NOT EXISTS transcript_segments (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  transcript_id BIGINT UNSIGNED NOT NULL,
  start_second INT NOT NULL DEFAULT 0,
  end_second INT NOT NULL DEFAULT 0,
  speaker VARCHAR(64) NOT NULL,
  content TEXT NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  reviewed_by BIGINT UNSIGNED NOT NULL DEFAULT 0,
  reviewed_at DATETIME(3) NULL,
  created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_segments_transcript (transcript_id),
  INDEX idx_segments_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
