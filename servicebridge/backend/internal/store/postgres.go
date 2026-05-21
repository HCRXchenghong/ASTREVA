package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"customer-service/backend/internal/domain"
)

var _ Store = (*PostgresStore)(nil)

const (
	defaultDBTimeout = 5 * time.Second

	adminColumns        = `id, account, name, password_hash, created_at`
	agentColumns        = `id, account, name, group_name, password_hash, status, max_conversations, current_conversations, disabled_at, created_at, updated_at`
	conversationColumns = `id, visitor_id, visitor_ip::text, visitor_remark, remark_updated_by, remark_updated_at, status, COALESCE(assigned_agent_id, ''), source, last_message, last_message_at, unread_for_agent, unread_for_visitor, created_at, updated_at`
	messageColumns      = `server_msg_id, client_msg_id, conversation_id, sender_type, sender_id, message_type, content, created_at, revoked_at, revoked_by_kind, revoked_by_id`
	ratingColumns       = `id, conversation_id, visitor_id, COALESCE(assigned_agent_id, ''), score, tags, comment, created_at`
	auditColumns        = `id, actor_kind, actor_id, action, resource, resource_id, ip_address, user_agent, description, created_at`
)

type PostgresStore struct {
	pool    *pgxpool.Pool
	options Options
}

type postgresOptions struct {
	MaxConns int32
}

type dbTx interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type scanTarget interface {
	Scan(...any) error
}

func NewPostgresStore(ctx context.Context, databaseURL string, options Options) (*PostgresStore, error) {
	return newPostgresStore(ctx, databaseURL, options, postgresOptions{MaxConns: 64})
}

func newPostgresStore(ctx context.Context, databaseURL string, options Options, opts postgresOptions) (*PostgresStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("database url is empty")
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}
	if cfg.MinConns == 0 && cfg.MaxConns >= 8 {
		cfg.MinConns = 4
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	s := &PostgresStore{pool: pool, options: options}
	if err := s.ensureServiceRatingsTable(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := s.seedDefaults(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *PostgresStore) LoginAdmin(account, password string) (domain.AuthSession, domain.AdminUser, error) {
	ctx, cancel := dbContext()
	defer cancel()

	admin, err := s.adminByAccount(ctx, account)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AuthSession{}, domain.AdminUser{}, ErrInvalidCredentials
		}
		return domain.AuthSession{}, domain.AdminUser{}, err
	}
	if !verifyPassword(admin.Password, password) {
		return domain.AuthSession{}, domain.AdminUser{}, ErrInvalidCredentials
	}
	session, err := s.createAuth(ctx, s.pool, domain.AccountAdmin, admin.ID, 24*time.Hour)
	if err != nil {
		return domain.AuthSession{}, domain.AdminUser{}, err
	}
	return session, admin, nil
}

func (s *PostgresStore) LoginAgent(account, password string) (domain.AuthSession, domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	agent, err := s.agentByAccount(ctx, account)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AuthSession{}, domain.Agent{}, ErrInvalidCredentials
		}
		return domain.AuthSession{}, domain.Agent{}, err
	}
	if agent.DisabledAt != nil {
		return domain.AuthSession{}, domain.Agent{}, ErrForbidden
	}
	if !verifyPassword(agent.Password, password) {
		return domain.AuthSession{}, domain.Agent{}, ErrInvalidCredentials
	}
	session, err := s.createAuth(ctx, s.pool, domain.AccountAgent, agent.ID, 24*time.Hour)
	if err != nil {
		return domain.AuthSession{}, domain.Agent{}, err
	}
	return session, agent, nil
}

func (s *PostgresStore) Auth(token string) (domain.AuthSession, bool) {
	session, ok := s.auth(token, false)
	return session, ok
}

func (s *PostgresStore) AuthVisitor(token string) (domain.AuthSession, bool) {
	session, ok := s.auth(token, true)
	return session, ok
}

func (s *PostgresStore) CreateVisitorConversation(ip, source string) (domain.VisitorSession, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.VisitorSession
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		now := dbNow()
		visitorID := "vis_" + randomID(8)
		conversationID := "conv_" + randomID(10)
		ip = ipForDB(ip)
		source = strings.TrimSpace(source)
		if source == "" {
			source = "web"
		}
		if _, err := cleanRequiredText(source, maxSourceRunes); err != nil {
			return err
		}

		visitor := domain.Visitor{
			ID:        visitorID,
			IP:        ip,
			Remark:    ip,
			Source:    source,
			CreatedAt: now,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO visitors (id, ip, remark, source, created_at)
			VALUES ($1, $2::inet, $3, $4, $5)
		`, visitor.ID, visitor.IP, visitor.Remark, visitor.Source, visitor.CreatedAt); err != nil {
			return err
		}

		conversation := domain.Conversation{
			ID:             conversationID,
			VisitorID:      visitorID,
			VisitorIP:      ip,
			VisitorRemark:  ip,
			Status:         domain.ConversationWaiting,
			Source:         source,
			LastMessage:    "新会话",
			LastMessageAt:  &now,
			UnreadForAgent: 0,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversations (
				id, visitor_id, visitor_ip, visitor_remark, status, source,
				last_message, last_message_at, unread_for_agent, unread_for_visitor, created_at, updated_at
			)
			VALUES ($1, $2, $3::inet, $4, $5, $6, $7, $8, 0, 0, $9, $10)
		`, conversation.ID, conversation.VisitorID, conversation.VisitorIP, conversation.VisitorRemark, conversation.Status, conversation.Source, conversation.LastMessage, conversation.LastMessageAt, conversation.CreatedAt, conversation.UpdatedAt); err != nil {
			return err
		}

		if err := s.assignConversationTx(ctx, tx, &conversation); err != nil {
			return err
		}
		if conversation.AssignedAgentID == "" {
			ai, err := s.aiSettingsTx(ctx, tx)
			if err != nil {
				return err
			}
			business, err := s.businessHoursTx(ctx, tx)
			if err != nil {
				return err
			}
			if ai.Enabled && ai.Mode != domain.AIModeManualOnly && (!business.Enabled || !isBusinessTime(business, now) || ai.Mode == domain.AIModeAlwaysAI) {
				conversation.Status = domain.ConversationAIServing
				if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": now}); err != nil {
					return err
				}
			}
		}

		initialMessages := []domain.Message{}
		contacts, err := s.contactSettings(ctx, tx)
		if err != nil {
			return err
		}
		if entryReply := strings.TrimSpace(contacts.EntryReply); entryReply != "" {
			msg, err := s.createMessageTx(ctx, tx, conversation.ID, "", domain.SenderAI, "ai", domain.MessageAIText, entryReply)
			if err != nil {
				return err
			}
			initialMessages = append(initialMessages, msg)
		}

		session, err := s.createAuth(ctx, tx, domain.AccountVisitor, conversation.ID, 30*24*time.Hour)
		if err != nil {
			return err
		}
		out = domain.VisitorSession{
			Token:           session.Token,
			Visitor:         visitor,
			Conversation:    conversation,
			InitialMessages: initialMessages,
		}
		return nil
	})
	return out, err
}

func (s *PostgresStore) SetAgentStatus(agentID string, status domain.AgentStatus) (domain.Agent, []domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	if err := validateAgentStatus(status); err != nil {
		return domain.Agent{}, nil, err
	}
	var out domain.Agent
	var assigned []domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		agent, err := s.agentByIDForUpdateTx(ctx, tx, agentID)
		if err != nil {
			return mapNoRows(err)
		}
		if agent.DisabledAt != nil && status == domain.AgentOnline {
			return ErrForbidden
		}
		now := dbNow()
		agent.Status = status
		agent.Updated = now
		if _, err := tx.Exec(ctx, `UPDATE agents SET status=$2, updated_at=$3 WHERE id=$1`, agent.ID, agent.Status, agent.Updated); err != nil {
			return err
		}

		if status == domain.AgentOffline {
			agent.CurrentConversations = 0
			if _, err := tx.Exec(ctx, `UPDATE agents SET current_conversations=0, updated_at=$2 WHERE id=$1`, agent.ID, now); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `
				SELECT `+conversationColumns+`
				FROM conversations
				WHERE assigned_agent_id=$1 AND status <> $2
				ORDER BY created_at
				FOR UPDATE
			`, agent.ID, domain.ConversationClosed)
			if err != nil {
				return err
			}
			conversations, err := scanConversations(rows)
			rows.Close()
			if err != nil {
				return err
			}
			for _, conversation := range conversations {
				conversation.AssignedAgentID = ""
				conversation.Status = domain.ConversationWaiting
				conversation.UpdatedAt = now
				if _, err := tx.Exec(ctx, `
					UPDATE conversations SET assigned_agent_id=NULL, status=$2, updated_at=$3 WHERE id=$1
				`, conversation.ID, conversation.Status, conversation.UpdatedAt); err != nil {
					return err
				}
				if err := s.assignConversationTx(ctx, tx, &conversation); err != nil {
					return err
				}
				if conversation.AssignedAgentID == "" {
					ai, err := s.aiSettingsTx(ctx, tx)
					if err != nil {
						return err
					}
					if ai.Enabled && ai.Mode != domain.AIModeManualOnly {
						conversation.Status = domain.ConversationAIServing
						conversation.UpdatedAt = now
						if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": now}); err != nil {
							return err
						}
					}
				}
				assigned = append(assigned, conversation)
			}
		}

		if status == domain.AgentOnline {
			for agent.CurrentConversations < agent.MaxConversations {
				conversation, err := s.nextWaitingConversationForUpdateTx(ctx, tx)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						break
					}
					return err
				}
				agent.CurrentConversations++
				conversation.AssignedAgentID = agent.ID
				conversation.Status = domain.ConversationAssigned
				conversation.UpdatedAt = now
				if _, err := tx.Exec(ctx, `
					UPDATE conversations SET assigned_agent_id=$2, status=$3, updated_at=$4 WHERE id=$1
				`, conversation.ID, conversation.AssignedAgentID, conversation.Status, conversation.UpdatedAt); err != nil {
					return err
				}
				assigned = append(assigned, conversation)
			}
			if _, err := tx.Exec(ctx, `UPDATE agents SET current_conversations=$2, updated_at=$3 WHERE id=$1`, agent.ID, agent.CurrentConversations, now); err != nil {
				return err
			}
		}
		out = agent
		return nil
	})
	return out, assigned, err
}

func (s *PostgresStore) ConversationsForAgent(agentID string) ([]domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	if _, err := s.agentByID(ctx, agentID); err != nil {
		return nil, mapNoRows(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE assigned_agent_id=$1 ORDER BY updated_at DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConversations(rows)
}

func (s *PostgresStore) AllConversations() []domain.Conversation {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT `+conversationColumns+` FROM conversations ORDER BY updated_at DESC LIMIT 500`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	conversations, err := scanConversations(rows)
	if err != nil {
		return nil
	}
	return conversations
}

func (s *PostgresStore) AllAgents() []domain.Agent {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT `+agentColumns+` FROM agents ORDER BY group_name, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	agents, err := scanAgents(rows)
	if err != nil {
		return nil
	}
	return agents
}

func (s *PostgresStore) Conversation(id string) (domain.Conversation, bool) {
	ctx, cancel := dbContext()
	defer cancel()

	conversation, err := s.conversationByID(ctx, id)
	return conversation, err == nil
}

func (s *PostgresStore) Messages(conversationID string) []domain.Message {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT `+messageColumns+` FROM messages WHERE conversation_id=$1 ORDER BY created_at`, conversationID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	messages, err := scanMessages(rows)
	if err != nil {
		return nil
	}
	return messages
}

func (s *PostgresStore) PagedMessages(conversationID string, limit int, beforeServerMsgID string) MessagePage {
	ctx, cancel := dbContext()
	defer cancel()

	if limit <= 0 || limit > 100 {
		limit = 50
	}
	beforeServerMsgID = strings.TrimSpace(beforeServerMsgID)
	rows, err := s.pool.Query(ctx, `
		SELECT `+messageColumns+`
		FROM (
			SELECT `+messageColumns+`
			FROM messages
			WHERE conversation_id=$1
				AND (
					$2 = ''
					OR (created_at, server_msg_id) < (
						SELECT created_at, server_msg_id
						FROM messages
						WHERE conversation_id=$1 AND server_msg_id=$2
					)
				)
			ORDER BY created_at DESC, server_msg_id DESC
			LIMIT $3
		) page
		ORDER BY created_at ASC, server_msg_id ASC
	`, conversationID, beforeServerMsgID, limit+1)
	if err != nil {
		return MessagePage{}
	}
	defer rows.Close()
	messages, err := scanMessages(rows)
	if err != nil {
		return MessagePage{}
	}
	page := MessagePage{Messages: messages}
	if len(messages) > limit {
		page.HasMore = true
		page.Messages = messages[1:]
	}
	if page.HasMore && len(page.Messages) > 0 {
		page.NextBefore = page.Messages[0].ServerMsgID
	}
	return page
}

func (s *PostgresStore) MarkConversationRead(actor domain.AuthSession, conversationID string) (domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if actor.Kind == domain.AccountAgent && conversation.AssignedAgentID != actor.AccountID {
			return ErrForbidden
		}
		if actor.Kind == domain.AccountVisitor && conversation.ID != actor.AccountID {
			return ErrForbidden
		}
		if conversation.UnreadForAgent == 0 {
			out = conversation
			return nil
		}
		row := tx.QueryRow(ctx, `UPDATE conversations SET unread_for_agent=0 WHERE id=$1 RETURNING `+conversationColumns, conversationID)
		out, err = scanConversation(row)
		return err
	})
	return out, err
}

func (s *PostgresStore) UpdateRemark(actor domain.AuthSession, conversationID, remark string) (domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if actor.Kind == domain.AccountAgent && conversation.AssignedAgentID != actor.AccountID {
			return ErrForbidden
		}
		remark = strings.TrimSpace(remark)
		if remark == "" {
			remark = conversation.VisitorIP
		}
		now := dbNow()
		row := tx.QueryRow(ctx, `
			UPDATE conversations
			SET visitor_remark=$2, remark_updated_by=$3, remark_updated_at=$4, updated_at=$4
			WHERE id=$1
			RETURNING `+conversationColumns, conversationID, remark, actor.AccountID, now)
		updated, err := scanConversation(row)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE visitors SET remark=$2 WHERE id=$1`, updated.VisitorID, updated.VisitorRemark); err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *PostgresStore) AddVisitorMessage(conversationID, clientMsgID, content string, messageType domain.MessageType) (MessageResult, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out MessageResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.Status == domain.ConversationClosed {
			return ErrForbidden
		}
		if err := validateClientMsgID(clientMsgID); err != nil {
			return err
		}
		if existing, ok, err := s.findMessageByClientIDTx(ctx, tx, conversationID, domain.SenderVisitor, conversation.VisitorID, clientMsgID); err != nil {
			return err
		} else if ok {
			out = MessageResult{Input: existing, Conversation: conversation}
			return nil
		}
		if err := validateUserMessage(messageType, content); err != nil {
			return err
		}

		input, err := s.createMessageTx(ctx, tx, conversationID, clientMsgID, domain.SenderVisitor, conversation.VisitorID, messageType, content)
		if err != nil {
			return err
		}
		if err := s.touchConversationTx(ctx, tx, &conversation, conversationPreviewText(messageType, content), true, input.CreatedAt); err != nil {
			return err
		}
		generated, needAI, err := s.generateAutoRepliesTx(ctx, tx, &conversation, content)
		if err != nil {
			return err
		}
		latest, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return err
		}
		out = MessageResult{
			Input:                input,
			Generated:            generated,
			Conversation:         latest,
			NeedAI:               needAI,
			AIText:               content,
			VisitorMessageSentAt: input.CreatedAt,
		}
		return nil
	})
	return out, err
}

func (s *PostgresStore) AddAIMessage(conversationID, content string) (domain.Message, domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var msg domain.Message
	var out domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.Status == domain.ConversationClosed {
			return ErrForbidden
		}
		conversation.Status = domain.ConversationAIServing
		if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": dbNow()}); err != nil {
			return err
		}
		msg, err = s.createMessageTx(ctx, tx, conversationID, "", domain.SenderAI, "ai", domain.MessageAIText, content)
		if err != nil {
			return err
		}
		if err := s.touchConversationTx(ctx, tx, &conversation, conversationPreviewText(domain.MessageAIText, content), false, msg.CreatedAt); err != nil {
			return err
		}
		out, err = s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		return err
	})
	return msg, out, err
}

func (s *PostgresStore) AddAgentMessage(agentID, conversationID, clientMsgID, content string, messageType domain.MessageType) (MessageResult, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out MessageResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.AssignedAgentID != agentID {
			return ErrForbidden
		}
		if conversation.Status == domain.ConversationClosed {
			return ErrForbidden
		}
		if err := validateClientMsgID(clientMsgID); err != nil {
			return err
		}
		if existing, ok, err := s.findMessageByClientIDTx(ctx, tx, conversationID, domain.SenderAgent, agentID, clientMsgID); err != nil {
			return err
		} else if ok {
			out = MessageResult{Input: existing, Conversation: conversation}
			return nil
		}
		if err := validateUserMessage(messageType, content); err != nil {
			return err
		}
		if conversation.Status == domain.ConversationAIServing || conversation.Status == domain.ConversationHumanRequested {
			conversation.Status = domain.ConversationAssigned
			if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": dbNow()}); err != nil {
				return err
			}
		}
		input, err := s.createMessageTx(ctx, tx, conversationID, clientMsgID, domain.SenderAgent, agentID, messageType, content)
		if err != nil {
			return err
		}
		if err := s.touchConversationTx(ctx, tx, &conversation, conversationPreviewText(messageType, content), false, input.CreatedAt); err != nil {
			return err
		}
		latest, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return err
		}
		out = MessageResult{Input: input, Conversation: latest}
		return nil
	})
	return out, err
}

func (s *PostgresStore) RevokeMessage(actor domain.AuthSession, conversationID, serverMsgID string) (domain.Message, domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var msg domain.Message
	var conversation domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		current, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if actor.Kind == domain.AccountAgent && current.AssignedAgentID != actor.AccountID {
			return ErrForbidden
		}
		if actor.Kind != domain.AccountAgent && actor.Kind != domain.AccountAdmin {
			return ErrForbidden
		}

		row := tx.QueryRow(ctx, `
			SELECT `+messageColumns+`
			FROM messages
			WHERE conversation_id=$1 AND server_msg_id=$2
			FOR UPDATE
		`, conversationID, strings.TrimSpace(serverMsgID))
		msg, err = scanMessage(row)
		if err != nil {
			return mapNoRows(err)
		}
		if msg.SenderType != domain.SenderAgent {
			return ErrForbidden
		}
		if actor.Kind == domain.AccountAgent && msg.SenderID != actor.AccountID {
			return ErrForbidden
		}
		if msg.RevokedAt == nil {
			now := dbNow()
			row = tx.QueryRow(ctx, `
				UPDATE messages
				SET revoked_at=$3, revoked_by_kind=$4, revoked_by_id=$5
				WHERE conversation_id=$1 AND server_msg_id=$2
				RETURNING `+messageColumns,
				conversationID,
				msg.ServerMsgID,
				now,
				actor.Kind,
				actor.AccountID,
			)
			msg, err = scanMessage(row)
			if err != nil {
				return err
			}
			if err := s.touchConversationRevokeTx(ctx, tx, &current, msg, now); err != nil {
				return err
			}
		}
		conversation, err = s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		return err
	})
	return msg, conversation, err
}

func (s *PostgresStore) EscalateNoReplyToAI(conversationID string, visitorMessageSentAt time.Time) (domain.Conversation, bool) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return err
		}
		if conversation.Status != domain.ConversationAssigned || conversation.LastMessageAt == nil {
			return ErrConflict
		}
		if !conversation.LastMessageAt.Equal(visitorMessageSentAt) {
			return ErrConflict
		}
		ai, err := s.aiSettingsTx(ctx, tx)
		if err != nil {
			return err
		}
		if !ai.Enabled || ai.Mode == domain.AIModeManualOnly {
			return ErrConflict
		}
		now := dbNow()
		row := tx.QueryRow(ctx, `UPDATE conversations SET status=$2, updated_at=$3 WHERE id=$1 RETURNING `+conversationColumns, conversationID, domain.ConversationAIServing, now)
		out, err = scanConversation(row)
		return err
	})
	return out, err == nil
}

func (s *PostgresStore) CloseConversation(actor domain.AuthSession, conversationID string) (domain.Conversation, domain.Message, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Conversation
	var msg domain.Message
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if actor.Kind == domain.AccountAgent && conversation.AssignedAgentID != actor.AccountID {
			return ErrForbidden
		}
		if actor.Kind == domain.AccountVisitor && conversation.ID != actor.AccountID {
			return ErrForbidden
		}
		now := dbNow()
		if conversation.AssignedAgentID != "" {
			if _, err := tx.Exec(ctx, `UPDATE agents SET current_conversations=GREATEST(current_conversations-1, 0), updated_at=$2 WHERE id=$1`, conversation.AssignedAgentID, now); err != nil {
				return err
			}
		}
		row := tx.QueryRow(ctx, `UPDATE conversations SET status=$2, updated_at=$3, closed_at=$3 WHERE id=$1 RETURNING `+conversationColumns, conversationID, domain.ConversationClosed, now)
		out, err = scanConversation(row)
		if err != nil {
			return err
		}
		msg, err = s.createMessageTx(ctx, tx, conversationID, "", domain.SenderSystem, "system", domain.MessageSystem, "会话已结束")
		return err
	})
	return out, msg, err
}

func (s *PostgresStore) DeleteConversation(actor domain.AuthSession, conversationID string) error {
	ctx, cancel := dbContext()
	defer cancel()

	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if actor.Kind == domain.AccountAgent && conversation.AssignedAgentID != actor.AccountID {
			return ErrForbidden
		}
		if actor.Kind == domain.AccountVisitor && conversation.ID != actor.AccountID {
			return ErrForbidden
		}
		if conversation.Status != domain.ConversationClosed {
			return ErrForbidden
		}
		_, err = tx.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, conversationID)
		return err
	})
}

func (s *PostgresStore) TransferConversation(actor domain.AuthSession, conversationID, agentID, group string) (domain.Conversation, domain.Message, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Conversation
	var msg domain.Message
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if actor.Kind != domain.AccountAdmin {
			return ErrForbidden
		}
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.Status == domain.ConversationClosed {
			return ErrForbidden
		}
		target, err := s.selectTransferTargetTx(ctx, tx, agentID, group)
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.AssignedAgentID == target.ID {
			msg, err = s.createMessageTx(ctx, tx, conversationID, "", domain.SenderSystem, "system", domain.MessageSystem, "会话已在目标客服名下。")
			out = conversation
			return err
		}
		now := dbNow()
		if conversation.AssignedAgentID != "" {
			if _, err := tx.Exec(ctx, `UPDATE agents SET current_conversations=GREATEST(current_conversations-1, 0), updated_at=$2 WHERE id=$1`, conversation.AssignedAgentID, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE agents SET current_conversations=current_conversations+1, updated_at=$2 WHERE id=$1`, target.ID, now); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `
			UPDATE conversations
			SET assigned_agent_id=$2, status=$3, updated_at=$4
			WHERE id=$1
			RETURNING `+conversationColumns, conversationID, target.ID, domain.ConversationAssigned, now)
		out, err = scanConversation(row)
		if err != nil {
			return err
		}
		msg, err = s.createMessageTx(ctx, tx, conversationID, "", domain.SenderSystem, "system", domain.MessageSystem, "会话已转接至 "+target.Name+"。")
		return err
	})
	return out, msg, err
}

func (s *PostgresStore) SubmitRating(conversationID string, score int, tags []string, comment string) (domain.ServiceRating, error) {
	ctx, cancel := dbContext()
	defer cancel()
	if score < 1 || score > 5 {
		return domain.ServiceRating{}, ErrInvalidInput
	}
	if err := validateRatingInput(tags, comment); err != nil {
		return domain.ServiceRating{}, err
	}
	var out domain.ServiceRating
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		row := tx.QueryRow(ctx, `
			INSERT INTO service_ratings (id, conversation_id, visitor_id, assigned_agent_id, score, tags, comment, created_at)
			VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8)
			RETURNING `+ratingColumns,
			"rate_"+randomID(8),
			conversation.ID,
			conversation.VisitorID,
			conversation.AssignedAgentID,
			score,
			cleanTags(tags),
			strings.TrimSpace(comment),
			dbNow(),
		)
		out, err = scanRating(row)
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	})
	return out, err
}

func (s *PostgresStore) RatingSummary() domain.RatingSummary {
	ctx, cancel := dbContext()
	defer cancel()

	var total int
	var average float64
	var satisfied int
	var neutral int
	var unsatisfied int
	err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COALESCE(AVG(score), 0)::float8,
			COUNT(*) FILTER (WHERE score >= 4)::int,
			COUNT(*) FILTER (WHERE score = 3)::int,
			COUNT(*) FILTER (WHERE score <= 2)::int
		FROM service_ratings
	`).Scan(&total, &average, &satisfied, &neutral, &unsatisfied)
	if err != nil || total == 0 {
		return domain.RatingSummary{}
	}
	return domain.RatingSummary{
		Total:            total,
		Average:          average,
		Satisfied:        satisfied,
		Neutral:          neutral,
		Unsatisfied:      unsatisfied,
		SatisfactionRate: float64(satisfied) / float64(total),
	}
}

func (s *PostgresStore) RecentRatings(limit int) []domain.ServiceRating {
	ctx, cancel := dbContext()
	defer cancel()
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `SELECT `+ratingColumns+` FROM service_ratings ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	ratings, err := scanRatings(rows)
	if err != nil {
		return nil
	}
	return ratings
}

func (s *PostgresStore) DashboardStats() domain.DashboardStats {
	ctx, cancel := dbContext()
	defer cancel()

	stats := domain.DashboardStats{Rating: s.RatingSummary()}
	rows, err := s.pool.Query(ctx, `SELECT status, COUNT(*)::int FROM conversations GROUP BY status`)
	if err == nil {
		for rows.Next() {
			var status domain.ConversationStatus
			var count int
			if err := rows.Scan(&status, &count); err != nil {
				continue
			}
			stats.TotalConversations += count
			switch status {
			case domain.ConversationAIServing:
				stats.AIServing = count
				stats.ActiveConversations += count
			case domain.ConversationWaiting:
				stats.Waiting = count
				stats.ActiveConversations += count
			case domain.ConversationHumanRequested:
				stats.HumanRequested = count
				stats.ActiveConversations += count
			case domain.ConversationAssigned:
				stats.Assigned = count
				stats.ActiveConversations += count
			case domain.ConversationClosed:
				stats.Closed = count
			}
		}
		rows.Close()
	}
	_ = s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE status=$1 AND disabled_at IS NULL)::int
		FROM agents
	`, domain.AgentOnline).Scan(&stats.TotalAgents, &stats.OnlineAgents)
	return stats
}

func (s *PostgresStore) RecordAuditEvent(event domain.AuditEvent) domain.AuditEvent {
	ctx, cancel := dbContext()
	defer cancel()

	if event.ID == "" {
		event.ID = "audit_" + randomID(8)
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = dbNow()
	}
	event.Action = strings.TrimSpace(event.Action)
	event.Resource = strings.TrimSpace(event.Resource)
	event.ResourceID = strings.TrimSpace(event.ResourceID)
	event.Description = strings.TrimSpace(event.Description)
	row := s.pool.QueryRow(ctx, `
		INSERT INTO audit_events (id, actor_kind, actor_id, action, resource, resource_id, ip_address, user_agent, description, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING `+auditColumns,
		event.ID, event.ActorKind, event.ActorID, event.Action, event.Resource, event.ResourceID, event.IPAddress, event.UserAgent, event.Description, event.CreatedAt)
	out, err := scanAuditEvent(row)
	if err != nil {
		return event
	}
	return out
}

func (s *PostgresStore) RecentAuditEvents(limit int) []domain.AuditEvent {
	ctx, cancel := dbContext()
	defer cancel()
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT `+auditColumns+` FROM audit_events ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	events, err := scanAuditEvents(rows)
	if err != nil {
		return nil
	}
	return events
}

func (s *PostgresStore) ContactSettings() domain.ContactSettings {
	ctx, cancel := dbContext()
	defer cancel()
	settings, err := s.contactSettings(ctx, s.pool)
	if err != nil {
		return domain.ContactSettings{}
	}
	return settings
}

func (s *PostgresStore) UpdateContactSettings(next domain.ContactSettings) (domain.ContactSettings, error) {
	ctx, cancel := dbContext()
	defer cancel()
	current, _ := s.contactSettings(ctx, s.pool)
	if err := validateContactSettings(next); err != nil {
		return current, err
	}
	if strings.TrimSpace(next.Phone) != "" {
		current.Phone = strings.TrimSpace(next.Phone)
	}
	if strings.TrimSpace(next.Wechat) != "" {
		current.Wechat = strings.TrimSpace(next.Wechat)
	}
	if normalizeReplyType(next.WechatReplyType) != "" {
		current.WechatReplyType = normalizeReplyType(next.WechatReplyType)
	}
	if strings.TrimSpace(next.WechatImageURL) != "" {
		current.WechatImageURL = strings.TrimSpace(next.WechatImageURL)
	}
	if strings.TrimSpace(next.QQ) != "" {
		current.QQ = strings.TrimSpace(next.QQ)
	}
	if normalizeReplyType(next.QQReplyType) != "" {
		current.QQReplyType = normalizeReplyType(next.QQReplyType)
	}
	if strings.TrimSpace(next.QQImageURL) != "" {
		current.QQImageURL = strings.TrimSpace(next.QQImageURL)
	}
	current.EntryReply = strings.TrimSpace(next.EntryReply)
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO contact_settings (id, phone, wechat, wechat_reply_type, wechat_image_url, qq, qq_reply_type, qq_image_url, entry_reply, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			phone=EXCLUDED.phone,
			wechat=EXCLUDED.wechat,
			wechat_reply_type=EXCLUDED.wechat_reply_type,
			wechat_image_url=EXCLUDED.wechat_image_url,
			qq=EXCLUDED.qq,
			qq_reply_type=EXCLUDED.qq_reply_type,
			qq_image_url=EXCLUDED.qq_image_url,
			entry_reply=EXCLUDED.entry_reply,
			updated_at=EXCLUDED.updated_at
	`, current.Phone, current.Wechat, nonEmpty(current.WechatReplyType, "text"), current.WechatImageURL, current.QQ, nonEmpty(current.QQReplyType, "text"), current.QQImageURL, current.EntryReply, dbNow())
	return current, nil
}

func (s *PostgresStore) KeywordRules() []domain.KeywordRule {
	ctx, cancel := dbContext()
	defer cancel()
	rules, err := s.keywordRules(ctx, s.pool)
	if err != nil {
		return nil
	}
	return rules
}

func (s *PostgresStore) CreateKeywordRule(next domain.KeywordRule) (domain.KeywordRule, error) {
	ctx, cancel := dbContext()
	defer cancel()
	next.Keyword = strings.TrimSpace(next.Keyword)
	next.Reply = strings.TrimSpace(next.Reply)
	if err := validateKeywordRule(next); err != nil {
		return domain.KeywordRule{}, err
	}
	if strings.TrimSpace(next.MatchType) == "" {
		next.MatchType = "contains"
	}
	if strings.TrimSpace(next.Action) == "" {
		next.Action = "text"
	}
	if next.ID == "" {
		next.ID = "kw_" + randomID(8)
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO keyword_rules (id, keyword, match_type, reply, enabled, priority, action, show_in_quick_replies, quick_reply_text, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
		RETURNING id, keyword, match_type, reply, enabled, priority, action, show_in_quick_replies, quick_reply_text
	`, next.ID, next.Keyword, next.MatchType, next.Reply, next.Enabled, next.Priority, next.Action, next.ShowInQuickReplies, strings.TrimSpace(next.QuickReplyText), dbNow()).Scan(&next.ID, &next.Keyword, &next.MatchType, &next.Reply, &next.Enabled, &next.Priority, &next.Action, &next.ShowInQuickReplies, &next.QuickReplyText)
	if isUniqueViolation(err) {
		return domain.KeywordRule{}, ErrConflict
	}
	return next, err
}

func (s *PostgresStore) UpdateKeywordRule(id string, next domain.KeywordRule) (domain.KeywordRule, error) {
	ctx, cancel := dbContext()
	defer cancel()
	current, err := s.keywordRuleByID(ctx, s.pool, id)
	if err != nil {
		return domain.KeywordRule{}, mapNoRows(err)
	}
	if strings.TrimSpace(next.Keyword) != "" {
		current.Keyword = strings.TrimSpace(next.Keyword)
	}
	if strings.TrimSpace(next.MatchType) != "" {
		current.MatchType = strings.TrimSpace(next.MatchType)
	}
	if strings.TrimSpace(next.Reply) != "" {
		current.Reply = strings.TrimSpace(next.Reply)
	}
	if strings.TrimSpace(next.Action) != "" {
		current.Action = strings.TrimSpace(next.Action)
	}
	current.ShowInQuickReplies = next.ShowInQuickReplies
	current.QuickReplyText = strings.TrimSpace(next.QuickReplyText)
	current.Enabled = next.Enabled
	current.Priority = next.Priority
	if err := validateKeywordRule(current); err != nil {
		return domain.KeywordRule{}, err
	}
	err = s.pool.QueryRow(ctx, `
		UPDATE keyword_rules
		SET keyword=$2, match_type=$3, reply=$4, enabled=$5, priority=$6, action=$7, show_in_quick_replies=$8, quick_reply_text=$9, updated_at=$10
		WHERE id=$1
		RETURNING id, keyword, match_type, reply, enabled, priority, action, show_in_quick_replies, quick_reply_text
	`, id, current.Keyword, current.MatchType, current.Reply, current.Enabled, current.Priority, current.Action, current.ShowInQuickReplies, current.QuickReplyText, dbNow()).Scan(&current.ID, &current.Keyword, &current.MatchType, &current.Reply, &current.Enabled, &current.Priority, &current.Action, &current.ShowInQuickReplies, &current.QuickReplyText)
	return current, err
}

func (s *PostgresStore) AISettings() domain.AISettings {
	ctx, cancel := dbContext()
	defer cancel()
	ai, err := s.aiSettingsTx(ctx, s.pool)
	if err != nil {
		return domain.AISettings{}
	}
	return ai
}

func (s *PostgresStore) UpdateAISettings(next domain.AISettings) (domain.AISettings, error) {
	ctx, cancel := dbContext()
	defer cancel()
	current, _ := s.aiSettingsTx(ctx, s.pool)
	if err := validateAISettings(next); err != nil {
		return current, err
	}
	if strings.TrimSpace(next.BaseURL) != "" {
		current.BaseURL = strings.TrimSpace(next.BaseURL)
	}
	if strings.TrimSpace(next.APIKey) != "" {
		current.APIKey = strings.TrimSpace(next.APIKey)
		current.APIKeyMasked = maskAPIKey(next.APIKey)
	}
	if strings.TrimSpace(next.Model) != "" {
		current.Model = strings.TrimSpace(next.Model)
	}
	if strings.TrimSpace(next.APIType) != "" {
		current.APIType = strings.TrimSpace(next.APIType)
	}
	if next.Mode != "" {
		current.Mode = next.Mode
	}
	current.Enabled = next.Enabled
	if next.Temperature >= 0 {
		current.Temperature = next.Temperature
	}
	if next.MaxOutputTokens > 0 {
		current.MaxOutputTokens = next.MaxOutputTokens
	}
	if next.TimeoutSeconds > 0 {
		current.TimeoutSeconds = next.TimeoutSeconds
	}
	if strings.TrimSpace(next.SystemPrompt) != "" {
		current.SystemPrompt = strings.TrimSpace(next.SystemPrompt)
	}
	if next.NoReplyTimeoutSeconds > 0 {
		current.NoReplyTimeoutSeconds = next.NoReplyTimeoutSeconds
	}
	protectedKey, err := protectSecret(s.options.DataEncryptionKey, current.APIKey)
	if err != nil {
		return current, err
	}
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO ai_settings (
			id, enabled, mode, base_url, api_key_ciphertext, model, api_type,
			temperature, max_output_tokens, timeout_seconds, system_prompt, agent_no_reply_timeout_seconds, updated_at
		)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET
			enabled=EXCLUDED.enabled,
			mode=EXCLUDED.mode,
			base_url=EXCLUDED.base_url,
			api_key_ciphertext=EXCLUDED.api_key_ciphertext,
			model=EXCLUDED.model,
			api_type=EXCLUDED.api_type,
			temperature=EXCLUDED.temperature,
			max_output_tokens=EXCLUDED.max_output_tokens,
			timeout_seconds=EXCLUDED.timeout_seconds,
			system_prompt=EXCLUDED.system_prompt,
			agent_no_reply_timeout_seconds=EXCLUDED.agent_no_reply_timeout_seconds,
			updated_at=EXCLUDED.updated_at
	`, current.Enabled, current.Mode, current.BaseURL, protectedKey, current.Model, current.APIType, current.Temperature, current.MaxOutputTokens, current.TimeoutSeconds, current.SystemPrompt, current.NoReplyTimeoutSeconds, dbNow())
	return current, nil
}

func (s *PostgresStore) BusinessHours() domain.BusinessHours {
	ctx, cancel := dbContext()
	defer cancel()
	business, err := s.businessHoursTx(ctx, s.pool)
	if err != nil {
		return domain.BusinessHours{}
	}
	return business
}

func (s *PostgresStore) UpdateBusinessHours(next domain.BusinessHours) (domain.BusinessHours, error) {
	ctx, cancel := dbContext()
	defer cancel()
	current, _ := s.businessHoursTx(ctx, s.pool)
	if err := validateBusinessHours(next); err != nil {
		return current, err
	}
	if strings.TrimSpace(next.Timezone) != "" {
		current.Timezone = strings.TrimSpace(next.Timezone)
	}
	if strings.TrimSpace(next.Start) != "" {
		current.Start = strings.TrimSpace(next.Start)
	}
	if strings.TrimSpace(next.End) != "" {
		current.End = strings.TrimSpace(next.End)
	}
	current.Enabled = next.Enabled
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO business_hours (id, timezone, start_time, end_time, enabled, updated_at)
		VALUES (1, $1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET timezone=EXCLUDED.timezone, start_time=EXCLUDED.start_time, end_time=EXCLUDED.end_time, enabled=EXCLUDED.enabled, updated_at=EXCLUDED.updated_at
	`, current.Timezone, current.Start, current.End, current.Enabled, dbNow())
	return current, nil
}

func (s *PostgresStore) CreateAgent(next domain.Agent, password string) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()
	next.Account = strings.TrimSpace(next.Account)
	next.Name = strings.TrimSpace(next.Name)
	next.Group = strings.TrimSpace(next.Group)
	password = strings.TrimSpace(password)
	if next.Account == "" || next.Name == "" || password == "" {
		return domain.Agent{}, ErrInvalidInput
	}
	if err := validateTemporaryPassword(password); err != nil {
		return domain.Agent{}, err
	}
	if next.ID == "" {
		next.ID = "agent_" + randomID(8)
	}
	if next.Group == "" {
		next.Group = "默认组"
	}
	if next.MaxConversations <= 0 {
		next.MaxConversations = 10
	}
	if next.Status == "" {
		next.Status = domain.AgentOffline
	}
	if err := validateAgentInput(next, true); err != nil {
		return domain.Agent{}, err
	}
	now := dbNow()
	hash, err := hashPassword(password)
	if err != nil {
		return domain.Agent{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO agents (id, account, name, group_name, password_hash, status, max_conversations, current_conversations, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, $8, $8)
		RETURNING `+agentColumns, next.ID, next.Account, next.Name, next.Group, hash, next.Status, next.MaxConversations, now)
	agent, err := scanAgent(row)
	if isUniqueViolation(err) {
		return domain.Agent{}, ErrConflict
	}
	return agent, err
}

func (s *PostgresStore) UpdateAgent(id string, next domain.Agent) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	if next.Name != "" || next.Group != "" || next.MaxConversations != 0 || next.Status != "" {
		if err := validateAgentInput(domain.Agent{
			Account:          "existing",
			Name:             nonEmpty(next.Name, "existing"),
			Group:            next.Group,
			Status:           next.Status,
			MaxConversations: next.MaxConversations,
		}, false); err != nil {
			return domain.Agent{}, err
		}
	}
	var out domain.Agent
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		agent, err := s.agentByIDForUpdateTx(ctx, tx, id)
		if err != nil {
			return mapNoRows(err)
		}
		if strings.TrimSpace(next.Name) != "" {
			agent.Name = strings.TrimSpace(next.Name)
		}
		if strings.TrimSpace(next.Group) != "" {
			agent.Group = strings.TrimSpace(next.Group)
		}
		if next.MaxConversations > 0 {
			agent.MaxConversations = next.MaxConversations
		}
		if next.Status != "" {
			if agent.DisabledAt != nil && next.Status == domain.AgentOnline {
				return ErrForbidden
			}
			agent.Status = next.Status
		}
		agent.Updated = dbNow()
		row := tx.QueryRow(ctx, `
			UPDATE agents
			SET name=$2, group_name=$3, status=$4, max_conversations=$5, updated_at=$6
			WHERE id=$1
			RETURNING `+agentColumns, agent.ID, agent.Name, agent.Group, agent.Status, agent.MaxConversations, agent.Updated)
		out, err = scanAgent(row)
		return err
	})
	return out, err
}

func (s *PostgresStore) ResetAgentPassword(id, password string) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()
	password = strings.TrimSpace(password)
	if password == "" {
		return domain.Agent{}, ErrInvalidInput
	}
	if err := validateTemporaryPassword(password); err != nil {
		return domain.Agent{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return domain.Agent{}, err
	}
	var agent domain.Agent
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `UPDATE agents SET password_hash=$2, updated_at=$3 WHERE id=$1 RETURNING `+agentColumns, id, hash, dbNow())
		updated, err := scanAgent(row)
		if err != nil {
			return mapNoRows(err)
		}
		agent = updated
		return s.revokeAuthSessionsTx(ctx, tx, domain.AccountAgent, id)
	})
	return agent, err
}

func (s *PostgresStore) ChangePassword(session domain.AuthSession, currentPassword, newPassword string) error {
	ctx, cancel := dbContext()
	defer cancel()

	currentPassword = strings.TrimSpace(currentPassword)
	newPassword = strings.TrimSpace(newPassword)
	if currentPassword == "" || validateNewPassword(newPassword) != nil {
		return ErrInvalidInput
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}

	switch session.Kind {
	case domain.AccountAdmin:
		return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			admin, err := scanAdmin(tx.QueryRow(ctx, `SELECT `+adminColumns+` FROM admin_users WHERE id=$1 FOR UPDATE`, session.AccountID))
			if err != nil {
				return mapNoRows(err)
			}
			if !verifyPassword(admin.Password, currentPassword) {
				return ErrInvalidCredentials
			}
			if _, err := tx.Exec(ctx, `UPDATE admin_users SET password_hash=$2 WHERE id=$1`, session.AccountID, hash); err != nil {
				return err
			}
			return s.revokeAuthSessionsTx(ctx, tx, domain.AccountAdmin, session.AccountID)
		})
	case domain.AccountAgent:
		return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			agent, err := s.agentByIDForUpdateTx(ctx, tx, session.AccountID)
			if err != nil {
				return mapNoRows(err)
			}
			if agent.DisabledAt != nil {
				return ErrForbidden
			}
			if !verifyPassword(agent.Password, currentPassword) {
				return ErrInvalidCredentials
			}
			if _, err := tx.Exec(ctx, `UPDATE agents SET password_hash=$2, updated_at=$3 WHERE id=$1`, session.AccountID, hash, dbNow()); err != nil {
				return err
			}
			return s.revokeAuthSessionsTx(ctx, tx, domain.AccountAgent, session.AccountID)
		})
	default:
		return ErrForbidden
	}
}

func (s *PostgresStore) DisableAgent(id string) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Agent
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		agent, err := s.agentByIDForUpdateTx(ctx, tx, id)
		if err != nil {
			return mapNoRows(err)
		}
		now := dbNow()
		row := tx.QueryRow(ctx, `
			UPDATE agents
			SET disabled_at=$2, status=$3, current_conversations=0, updated_at=$2
			WHERE id=$1
			RETURNING `+agentColumns, agent.ID, now, domain.AgentOffline)
		out, err = scanAgent(row)
		if err != nil {
			return err
		}
		if err := s.revokeAuthSessionsTx(ctx, tx, domain.AccountAgent, agent.ID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT `+conversationColumns+`
			FROM conversations
			WHERE assigned_agent_id=$1 AND status <> $2
			ORDER BY created_at
			FOR UPDATE
		`, id, domain.ConversationClosed)
		if err != nil {
			return err
		}
		conversations, err := scanConversations(rows)
		rows.Close()
		if err != nil {
			return err
		}
		for _, conversation := range conversations {
			conversation.AssignedAgentID = ""
			conversation.Status = domain.ConversationWaiting
			conversation.UpdatedAt = now
			if _, err := tx.Exec(ctx, `
				UPDATE conversations SET assigned_agent_id=NULL, status=$2, updated_at=$3 WHERE id=$1
			`, conversation.ID, conversation.Status, conversation.UpdatedAt); err != nil {
				return err
			}
			if err := s.assignConversationTx(ctx, tx, &conversation); err != nil {
				return err
			}
			if conversation.AssignedAgentID == "" {
				ai, err := s.aiSettingsTx(ctx, tx)
				if err != nil {
					return err
				}
				if ai.Enabled && ai.Mode != domain.AIModeManualOnly {
					if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": domain.ConversationAIServing, "updated_at": now}); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	return out, err
}

func (s *PostgresStore) DeleteAgent(id string) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Agent
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		agent, err := s.agentByIDForUpdateTx(ctx, tx, id)
		if err != nil {
			return mapNoRows(err)
		}
		now := dbNow()
		if _, err := tx.Exec(ctx, `
			UPDATE agents
			SET status=$2, current_conversations=0, updated_at=$3
			WHERE id=$1
		`, agent.ID, domain.AgentOffline, now); err != nil {
			return err
		}
		if err := s.revokeAuthSessionsTx(ctx, tx, domain.AccountAgent, agent.ID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT `+conversationColumns+`
			FROM conversations
			WHERE assigned_agent_id=$1 AND status <> $2
			ORDER BY created_at
			FOR UPDATE
		`, id, domain.ConversationClosed)
		if err != nil {
			return err
		}
		conversations, err := scanConversations(rows)
		rows.Close()
		if err != nil {
			return err
		}
		for _, conversation := range conversations {
			conversation.AssignedAgentID = ""
			conversation.Status = domain.ConversationWaiting
			conversation.UpdatedAt = now
			if _, err := tx.Exec(ctx, `
				UPDATE conversations SET assigned_agent_id=NULL, status=$2, updated_at=$3 WHERE id=$1
			`, conversation.ID, conversation.Status, conversation.UpdatedAt); err != nil {
				return err
			}
			if err := s.assignConversationTx(ctx, tx, &conversation); err != nil {
				return err
			}
			if conversation.AssignedAgentID == "" {
				ai, err := s.aiSettingsTx(ctx, tx)
				if err != nil {
					return err
				}
				if ai.Enabled && ai.Mode != domain.AIModeManualOnly {
					if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": domain.ConversationAIServing, "updated_at": now}); err != nil {
						return err
					}
				}
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET assigned_agent_id=NULL, updated_at=$2 WHERE assigned_agent_id=$1`, agent.ID, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE service_ratings SET assigned_agent_id='' WHERE assigned_agent_id=$1`, agent.ID); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `DELETE FROM agents WHERE id=$1 RETURNING `+agentColumns, agent.ID)
		out, err = scanAgent(row)
		return err
	})
	return out, err
}

func (s *PostgresStore) RegisterAgentPushDevice(agentID string, device domain.PushDevice) (domain.PushDevice, error) {
	ctx, cancel := dbContext()
	defer cancel()
	if _, err := s.agentByID(ctx, agentID); err != nil {
		return domain.PushDevice{}, mapNoRows(err)
	}
	device.Token = strings.TrimSpace(device.Token)
	device.Platform = strings.TrimSpace(device.Platform)
	device.Provider = strings.TrimSpace(device.Provider)
	if err := validatePushDevice(device); err != nil {
		return domain.PushDevice{}, err
	}
	if device.ID == "" {
		device.ID = "push_" + randomID(8)
	}
	if device.Provider == "" {
		device.Provider = "uni-push"
	}
	now := dbNow()
	row := s.pool.QueryRow(ctx, `
		INSERT INTO push_devices (id, agent_id, platform, provider, token, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, true, $6, $6)
		ON CONFLICT (agent_id, provider, platform, token) DO UPDATE
		SET enabled=true, updated_at=EXCLUDED.updated_at
		RETURNING id, agent_id, platform, token, provider, enabled, updated_at, created_at
	`, device.ID, agentID, device.Platform, device.Provider, device.Token, now)
	return scanPushDevice(row)
}

func (s *PostgresStore) PushDevicesForAgent(agentID string) []domain.PushDevice {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, platform, token, provider, enabled, updated_at, created_at
		FROM push_devices
		WHERE agent_id=$1 AND enabled=true
		ORDER BY updated_at DESC
	`, agentID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	devices := []domain.PushDevice{}
	for rows.Next() {
		device, err := scanPushDevice(rows)
		if err != nil {
			return devices
		}
		devices = append(devices, device)
	}
	return devices
}

func (s *PostgresStore) adminByAccount(ctx context.Context, account string) (domain.AdminUser, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+adminColumns+` FROM admin_users WHERE account=$1`, strings.TrimSpace(account))
	return scanAdmin(row)
}

func (s *PostgresStore) adminByID(ctx context.Context, id string) (domain.AdminUser, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+adminColumns+` FROM admin_users WHERE id=$1`, id)
	return scanAdmin(row)
}

func (s *PostgresStore) agentByAccount(ctx context.Context, account string) (domain.Agent, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+agentColumns+` FROM agents WHERE account=$1`, strings.TrimSpace(account))
	return scanAgent(row)
}

func (s *PostgresStore) agentByID(ctx context.Context, id string) (domain.Agent, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+agentColumns+` FROM agents WHERE id=$1`, id)
	return scanAgent(row)
}

func (s *PostgresStore) agentByIDForUpdateTx(ctx context.Context, tx pgx.Tx, id string) (domain.Agent, error) {
	row := tx.QueryRow(ctx, `SELECT `+agentColumns+` FROM agents WHERE id=$1 FOR UPDATE`, id)
	return scanAgent(row)
}

func (s *PostgresStore) conversationByID(ctx context.Context, id string) (domain.Conversation, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE id=$1`, id)
	return scanConversation(row)
}

func (s *PostgresStore) conversationByIDForUpdateTx(ctx context.Context, tx pgx.Tx, id string) (domain.Conversation, error) {
	row := tx.QueryRow(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE id=$1 FOR UPDATE`, id)
	return scanConversation(row)
}

func (s *PostgresStore) nextWaitingConversationForUpdateTx(ctx context.Context, tx pgx.Tx) (domain.Conversation, error) {
	row := tx.QueryRow(ctx, `
		SELECT `+conversationColumns+`
		FROM conversations
		WHERE status IN ($1, $2)
		ORDER BY created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, domain.ConversationWaiting, domain.ConversationHumanRequested)
	return scanConversation(row)
}

func (s *PostgresStore) createAuth(ctx context.Context, exec dbTx, kind domain.AccountKind, accountID string, ttl time.Duration) (domain.AuthSession, error) {
	now := dbNow()
	prefix := "a_"
	if kind == domain.AccountVisitor {
		prefix = "v_"
	}
	session := domain.AuthSession{
		Token:     prefix + randomID(24),
		Kind:      kind,
		AccountID: accountID,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	_, err := exec.Exec(ctx, `
		INSERT INTO auth_sessions (token, kind, account_id, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, session.Token, session.Kind, session.AccountID, session.CreatedAt, session.ExpiresAt)
	return session, err
}

func (s *PostgresStore) revokeAuthSessionsTx(ctx context.Context, exec dbTx, kind domain.AccountKind, accountID string) error {
	_, err := exec.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at=now()
		WHERE kind=$1 AND account_id=$2 AND revoked_at IS NULL
	`, kind, accountID)
	return err
}

func (s *PostgresStore) auth(token string, visitor bool) (domain.AuthSession, bool) {
	ctx, cancel := dbContext()
	defer cancel()
	kinds := []domain.AccountKind{domain.AccountAdmin, domain.AccountAgent}
	if visitor {
		kinds = []domain.AccountKind{domain.AccountVisitor}
	}
	row := s.pool.QueryRow(ctx, `
		SELECT token, kind, account_id, created_at, expires_at
		FROM auth_sessions
		WHERE token=$1 AND kind=ANY($2) AND revoked_at IS NULL AND expires_at > now()
	`, strings.TrimSpace(token), kinds)
	var session domain.AuthSession
	if err := row.Scan(&session.Token, &session.Kind, &session.AccountID, &session.CreatedAt, &session.ExpiresAt); err != nil {
		return domain.AuthSession{}, false
	}
	return session, true
}

func (s *PostgresStore) assignConversationTx(ctx context.Context, tx pgx.Tx, conversation *domain.Conversation) error {
	ai, err := s.aiSettingsTx(ctx, tx)
	if err != nil {
		return err
	}
	now := dbNow()
	if ai.Mode == domain.AIModeAlwaysAI {
		conversation.Status = domain.ConversationAIServing
		conversation.UpdatedAt = now
		return s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": now})
	}
	row := tx.QueryRow(ctx, `
		SELECT `+agentColumns+`
		FROM agents
		WHERE status=$1
			AND disabled_at IS NULL
			AND current_conversations < max_conversations
		ORDER BY current_conversations ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, domain.AgentOnline)
	agent, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE agents SET current_conversations=current_conversations+1, updated_at=$2 WHERE id=$1`, agent.ID, now); err != nil {
		return err
	}
	conversation.AssignedAgentID = agent.ID
	conversation.Status = domain.ConversationAssigned
	conversation.UpdatedAt = now
	return s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{
		"assigned_agent_id": conversation.AssignedAgentID,
		"status":            conversation.Status,
		"updated_at":        now,
	})
}

func (s *PostgresStore) selectTransferTargetTx(ctx context.Context, tx pgx.Tx, agentID, group string) (domain.Agent, error) {
	agentID = strings.TrimSpace(agentID)
	group = strings.TrimSpace(group)
	if agentID != "" {
		row := tx.QueryRow(ctx, `SELECT `+agentColumns+` FROM agents WHERE id=$1 AND disabled_at IS NULL FOR UPDATE`, agentID)
		return scanAgent(row)
	}
	row := tx.QueryRow(ctx, `
		SELECT `+agentColumns+`
		FROM agents
		WHERE disabled_at IS NULL
			AND status=$1
			AND current_conversations < max_conversations
			AND ($2 = '' OR group_name = $2)
		ORDER BY current_conversations ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, domain.AgentOnline, group)
	return scanAgent(row)
}

func (s *PostgresStore) createMessageTx(ctx context.Context, tx pgx.Tx, conversationID, clientMsgID string, senderType domain.SenderType, senderID string, messageType domain.MessageType, content string) (domain.Message, error) {
	msg := domain.Message{
		ServerMsgID:    "msg_" + randomID(12),
		ClientMsgID:    strings.TrimSpace(clientMsgID),
		ConversationID: conversationID,
		SenderType:     senderType,
		SenderID:       senderID,
		MessageType:    messageType,
		Content:        strings.TrimSpace(content),
		CreatedAt:      dbNow(),
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO messages (server_msg_id, client_msg_id, conversation_id, sender_type, sender_id, message_type, content, created_at, revoked_at, revoked_by_kind, revoked_by_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, '', '')
		RETURNING `+messageColumns, msg.ServerMsgID, msg.ClientMsgID, msg.ConversationID, msg.SenderType, msg.SenderID, msg.MessageType, msg.Content, msg.CreatedAt)
	return scanMessage(row)
}

func (s *PostgresStore) findMessageByClientIDTx(ctx context.Context, tx pgx.Tx, conversationID string, senderType domain.SenderType, senderID, clientMsgID string) (domain.Message, bool, error) {
	clientMsgID = strings.TrimSpace(clientMsgID)
	if clientMsgID == "" {
		return domain.Message{}, false, nil
	}
	row := tx.QueryRow(ctx, `
		SELECT `+messageColumns+`
		FROM messages
		WHERE conversation_id=$1 AND sender_type=$2 AND sender_id=$3 AND client_msg_id=$4
	`, conversationID, senderType, senderID, clientMsgID)
	msg, err := scanMessage(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Message{}, false, nil
		}
		return domain.Message{}, false, err
	}
	return msg, true, nil
}

func (s *PostgresStore) touchConversationTx(ctx context.Context, tx pgx.Tx, conversation *domain.Conversation, content string, fromVisitor bool, at time.Time) error {
	now := at.UTC().Truncate(time.Microsecond)
	conversation.LastMessage = strings.TrimSpace(content)
	conversation.LastMessageAt = &now
	conversation.UpdatedAt = now
	if fromVisitor {
		conversation.UnreadForAgent++
		row := tx.QueryRow(ctx, `
			UPDATE conversations
			SET last_message=$2, last_message_at=$3, updated_at=$3, unread_for_agent=unread_for_agent+1
			WHERE id=$1
			RETURNING `+conversationColumns, conversation.ID, conversation.LastMessage, now)
		updated, err := scanConversation(row)
		if err != nil {
			return err
		}
		*conversation = updated
		return nil
	}
	conversation.UnreadForVisitor++
	conversation.UnreadForAgent = 0
	row := tx.QueryRow(ctx, `
		UPDATE conversations
		SET last_message=$2, last_message_at=$3, updated_at=$3, unread_for_visitor=unread_for_visitor+1, unread_for_agent=0
		WHERE id=$1
		RETURNING `+conversationColumns, conversation.ID, conversation.LastMessage, now)
	updated, err := scanConversation(row)
	if err != nil {
		return err
	}
	*conversation = updated
	return nil
}

func (s *PostgresStore) touchConversationRevokeTx(ctx context.Context, tx pgx.Tx, conversation *domain.Conversation, msg domain.Message, at time.Time) error {
	now := at.UTC().Truncate(time.Microsecond)
	conversation.LastMessage = "已撤回一条消息"
	conversation.LastMessageAt = &now
	conversation.UpdatedAt = now
	row := tx.QueryRow(ctx, `
		UPDATE conversations
		SET last_message=$2,
			last_message_at=$3,
			updated_at=$3,
			unread_for_visitor=GREATEST(unread_for_visitor - $4, 0)
		WHERE id=$1
		RETURNING `+conversationColumns,
		conversation.ID,
		conversation.LastMessage,
		now,
		boolToInt(msg.SenderType == domain.SenderAgent),
	)
	updated, err := scanConversation(row)
	if err != nil {
		return err
	}
	*conversation = updated
	return nil
}

func (s *PostgresStore) generateAutoRepliesTx(ctx context.Context, tx pgx.Tx, conversation *domain.Conversation, content string) ([]domain.Message, bool, error) {
	normalized := strings.TrimSpace(content)
	if normalized == "" {
		return nil, false, nil
	}
	generated := []domain.Message{}
	if isHumanKeyword(normalized) {
		if conversation.AssignedAgentID != "" {
			conversation.Status = domain.ConversationAssigned
			if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": dbNow()}); err != nil {
				return nil, false, err
			}
			msg, err := s.createMessageTx(ctx, tx, conversation.ID, "", domain.SenderSystem, "system", domain.MessageHandoff, "已为您通知人工客服，请稍候。")
			if err != nil {
				return nil, false, err
			}
			return append(generated, msg), false, nil
		}
		conversation.Status = domain.ConversationHumanRequested
		if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": dbNow()}); err != nil {
			return nil, false, err
		}
		if err := s.assignConversationTx(ctx, tx, conversation); err != nil {
			return nil, false, err
		}
		if conversation.AssignedAgentID != "" {
			msg, err := s.createMessageTx(ctx, tx, conversation.ID, "", domain.SenderSystem, "system", domain.MessageHandoff, "已为您通知人工客服，请稍候。")
			if err != nil {
				return nil, false, err
			}
			return append(generated, msg), false, nil
		}
		msg, err := s.createMessageTx(ctx, tx, conversation.ID, "", domain.SenderAI, "ai", domain.MessageAIText, "已为您通知人工客服。当前人工暂时不可用，我会先继续帮您处理常见问题。")
		if err != nil {
			return nil, false, err
		}
		return append(generated, msg), false, nil
	}

	rules, err := s.keywordRules(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	for _, rule := range rules {
		if !rule.Enabled || !keywordMatches(rule, normalized) {
			continue
		}
		messageType := domain.MessageText
		if rule.Action == "phone" {
			messageType = domain.MessageContactPhone
		}
		if rule.Action == "wechat" {
			messageType = domain.MessageContactWechat
		}
		if rule.Action == "handoff" {
			messageType = domain.MessageHandoff
		}
		msg, err := s.createMessageTx(ctx, tx, conversation.ID, "", domain.SenderSystem, "system", messageType, rule.Reply)
		if err != nil {
			return nil, false, err
		}
		return append(generated, msg), false, nil
	}

	if conversation.Status == domain.ConversationAssigned {
		return generated, false, nil
	}
	ai, err := s.aiSettingsTx(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	if !ai.Enabled || ai.Mode == domain.AIModeManualOnly {
		return generated, false, nil
	}
	business, err := s.businessHoursTx(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	if conversation.Status == domain.ConversationWaiting && business.Enabled && isBusinessTime(business, time.Now().UTC()) {
		return generated, false, nil
	}
	conversation.Status = domain.ConversationAIServing
	if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": dbNow()}); err != nil {
		return nil, false, err
	}
	return generated, true, nil
}

func (s *PostgresStore) updateConversationFieldsTx(ctx context.Context, tx pgx.Tx, conversationID string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	allowed := map[string]bool{
		"assigned_agent_id": true,
		"status":            true,
		"updated_at":        true,
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		if !allowed[key] {
			return fmt.Errorf("unsupported conversation field: %s", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := []any{conversationID}
	sets := make([]string, 0, len(keys))
	for _, key := range keys {
		args = append(args, fields[key])
		if key == "assigned_agent_id" && fields[key] == "" {
			sets = append(sets, fmt.Sprintf("%s=NULL", key))
			args = args[:len(args)-1]
			continue
		}
		sets = append(sets, fmt.Sprintf("%s=$%d", key, len(args)))
	}
	query := `UPDATE conversations SET ` + strings.Join(sets, ", ") + ` WHERE id=$1`
	_, err := tx.Exec(ctx, query, args...)
	return err
}

func (s *PostgresStore) contactSettings(ctx context.Context, exec dbTx) (domain.ContactSettings, error) {
	var out domain.ContactSettings
	err := exec.QueryRow(ctx, `
		SELECT phone, wechat, wechat_reply_type, wechat_image_url, qq, qq_reply_type, qq_image_url, entry_reply
		FROM contact_settings WHERE id=1
	`).Scan(&out.Phone, &out.Wechat, &out.WechatReplyType, &out.WechatImageURL, &out.QQ, &out.QQReplyType, &out.QQImageURL, &out.EntryReply)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ContactSettings{
			Phone:           "400-123-4567",
			Wechat:          "+86 400 888 8888",
			WechatReplyType: "text",
			QQ:              "@AstrevaSupport",
			QQReplyType:     "text",
			EntryReply:      "您好，欢迎咨询在线客服，请问有什么可以帮您？",
		}, nil
	}
	return out, err
}

func (s *PostgresStore) keywordRules(ctx context.Context, exec dbTx) ([]domain.KeywordRule, error) {
	rows, err := exec.Query(ctx, `
		SELECT id, keyword, match_type, reply, enabled, priority, action, show_in_quick_replies, quick_reply_text
		FROM keyword_rules
		ORDER BY priority DESC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.KeywordRule{}
	for rows.Next() {
		var rule domain.KeywordRule
		if err := rows.Scan(
			&rule.ID,
			&rule.Keyword,
			&rule.MatchType,
			&rule.Reply,
			&rule.Enabled,
			&rule.Priority,
			&rule.Action,
			&rule.ShowInQuickReplies,
			&rule.QuickReplyText,
		); err != nil {
			return nil, err
		}
		result = append(result, rule)
	}
	return result, rows.Err()
}

func (s *PostgresStore) keywordRuleByID(ctx context.Context, exec dbTx, id string) (domain.KeywordRule, error) {
	var rule domain.KeywordRule
	err := exec.QueryRow(ctx, `
		SELECT id, keyword, match_type, reply, enabled, priority, action, show_in_quick_replies, quick_reply_text
		FROM keyword_rules
		WHERE id=$1
	`, id).Scan(
		&rule.ID,
		&rule.Keyword,
		&rule.MatchType,
		&rule.Reply,
		&rule.Enabled,
		&rule.Priority,
		&rule.Action,
		&rule.ShowInQuickReplies,
		&rule.QuickReplyText,
	)
	return rule, err
}

func (s *PostgresStore) aiSettingsTx(ctx context.Context, exec dbTx) (domain.AISettings, error) {
	var out domain.AISettings
	var storedKey string
	err := exec.QueryRow(ctx, `
		SELECT enabled, mode, base_url, api_key_ciphertext, model, api_type, temperature, max_output_tokens, timeout_seconds, system_prompt, agent_no_reply_timeout_seconds
		FROM ai_settings WHERE id=1
	`).Scan(&out.Enabled, &out.Mode, &out.BaseURL, &storedKey, &out.Model, &out.APIType, &out.Temperature, &out.MaxOutputTokens, &out.TimeoutSeconds, &out.SystemPrompt, &out.NoReplyTimeoutSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		out = domain.AISettings{
			Enabled:               true,
			Mode:                  domain.AIModeHumanFirst,
			BaseURL:               nonEmpty(s.options.OpenAIBaseURL, "https://api.openai.com/v1"),
			APIKey:                strings.TrimSpace(s.options.OpenAIAPIKey),
			Model:                 nonEmpty(s.options.OpenAIModel, "gpt-4o-mini"),
			APIType:               nonEmpty(s.options.OpenAIAPIType, "chat_completions"),
			Temperature:           0.7,
			MaxOutputTokens:       512,
			TimeoutSeconds:        20,
			SystemPrompt:          defaultPrompt(),
			NoReplyTimeoutSeconds: 60,
		}
		out.APIKeyMasked = maskAPIKey(out.APIKey)
		return out, nil
	}
	apiKey, err := revealSecret(s.options.DataEncryptionKey, storedKey)
	if err != nil {
		return domain.AISettings{}, err
	}
	out.APIKey = apiKey
	out.APIKeyMasked = maskAPIKey(out.APIKey)
	return out, err
}

func (s *PostgresStore) businessHoursTx(ctx context.Context, exec dbTx) (domain.BusinessHours, error) {
	var out domain.BusinessHours
	err := exec.QueryRow(ctx, `SELECT timezone, start_time, end_time, enabled FROM business_hours WHERE id=1`).
		Scan(&out.Timezone, &out.Start, &out.End, &out.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BusinessHours{Timezone: "Asia/Shanghai", Start: "09:00", End: "18:00", Enabled: true}, nil
	}
	return out, err
}

func (s *PostgresStore) seedDefaults(ctx context.Context) error {
	adminHash, err := hashPassword(nonEmpty(s.options.BootstrapAdminPassword, "123456"))
	if err != nil {
		return err
	}
	agentHash, err := hashPassword(nonEmpty(s.options.BootstrapAgentPassword, "123456"))
	if err != nil {
		return err
	}
	protectedSeedKey, err := protectSecret(s.options.DataEncryptionKey, strings.TrimSpace(s.options.OpenAIAPIKey))
	if err != nil {
		return err
	}
	statements := []struct {
		sql  string
		args []any
	}{
		{
			sql: `
				INSERT INTO admin_users (id, account, name, password_hash)
				VALUES ('admin_super', 'superadmin', '超级管理员', $1)
				ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash
				WHERE admin_users.password_hash LIKE 'dev_plain:%'
			`,
			args: []any{adminHash},
		},
		{
			sql: `
				INSERT INTO agents (id, account, name, group_name, password_hash, status, max_conversations)
				VALUES ('agent_lixue', 'admin', '客服-李雪', '售前组', $1, 'offline', 10)
				ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash
				WHERE agents.password_hash LIKE 'dev_plain:%'
			`,
			args: []any{agentHash},
		},
		{
			sql: `
				INSERT INTO keyword_rules (id, keyword, match_type, reply, enabled, priority, action)
				VALUES
					('kw_phone', '电话', 'contains', '客服电话：400-123-4567', true, 90, 'phone'),
					('kw_whatsapp', 'WhatsApp', 'contains', '官方 WhatsApp：+86 400 888 8888', true, 80, 'wechat'),
					('kw_telegram', 'Telegram', 'contains', '官方 Telegram：@AstrevaSupport', true, 75, 'text')
				ON CONFLICT (id) DO NOTHING
			`,
		},
		{
			sql: `
				INSERT INTO contact_settings (id, phone, wechat, wechat_reply_type, qq, qq_reply_type, entry_reply)
				VALUES (1, '400-123-4567', '+86 400 888 8888', 'text', '@AstrevaSupport', 'text', '您好，欢迎咨询在线客服，请问有什么可以帮您？')
				ON CONFLICT (id) DO NOTHING
			`,
		},
		{
			sql: `
				INSERT INTO ai_settings (id, enabled, mode, base_url, api_key_ciphertext, model, api_type, temperature, max_output_tokens, timeout_seconds, system_prompt, agent_no_reply_timeout_seconds)
				VALUES (1, true, 'human_first', $1, $2, $3, $4, 0.70, 512, 20, $5, 60)
				ON CONFLICT (id) DO NOTHING
			`,
			args: []any{
				nonEmpty(s.options.OpenAIBaseURL, "https://api.openai.com/v1"),
				protectedSeedKey,
				nonEmpty(s.options.OpenAIModel, "gpt-4o-mini"),
				nonEmpty(s.options.OpenAIAPIType, "chat_completions"),
				defaultPrompt(),
			},
		},
		{
			sql: `
				INSERT INTO business_hours (id, timezone, start_time, end_time, enabled)
				VALUES (1, 'Asia/Shanghai', '09:00', '18:00', true)
				ON CONFLICT (id) DO NOTHING
			`,
		},
	}
	for _, statement := range statements {
		if _, err := s.pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) ensureServiceRatingsTable(ctx context.Context) error {
	statements := []string{
		`ALTER TABLE contact_settings ADD COLUMN IF NOT EXISTS wechat_reply_type TEXT NOT NULL DEFAULT 'image'`,
		`ALTER TABLE contact_settings ADD COLUMN IF NOT EXISTS wechat_image_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE contact_settings ADD COLUMN IF NOT EXISTS qq_reply_type TEXT NOT NULL DEFAULT 'text'`,
		`ALTER TABLE contact_settings ADD COLUMN IF NOT EXISTS qq_image_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE contact_settings ADD COLUMN IF NOT EXISTS entry_reply TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE contact_settings DROP CONSTRAINT IF EXISTS contact_settings_wechat_reply_type_check`,
		`ALTER TABLE contact_settings ADD CONSTRAINT contact_settings_wechat_reply_type_check CHECK (wechat_reply_type IN ('text', 'image'))`,
		`ALTER TABLE contact_settings DROP CONSTRAINT IF EXISTS contact_settings_qq_reply_type_check`,
		`ALTER TABLE contact_settings ADD CONSTRAINT contact_settings_qq_reply_type_check CHECK (qq_reply_type IN ('text', 'image'))`,
		`UPDATE contact_settings SET entry_reply='您好，欢迎咨询在线客服，请问有什么可以帮您？' WHERE id=1 AND entry_reply=''`,
		`ALTER TABLE keyword_rules ADD COLUMN IF NOT EXISTS show_in_quick_replies BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE keyword_rules ADD COLUMN IF NOT EXISTS quick_reply_text TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS revoked_by_kind TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS revoked_by_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_message_type_check`,
		`ALTER TABLE messages ADD CONSTRAINT messages_message_type_check CHECK (message_type IN ('text', 'emoji', 'image', 'audio', 'contact_phone', 'contact_wechat', 'system', 'ai_text', 'handoff_request', 'revoked'))`,
		`CREATE TABLE IF NOT EXISTS service_ratings (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL UNIQUE REFERENCES conversations(id) ON DELETE CASCADE,
			visitor_id TEXT NOT NULL REFERENCES visitors(id),
			assigned_agent_id TEXT,
			score INTEGER NOT NULL,
			tags TEXT[] NOT NULL DEFAULT '{}',
			comment TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT service_ratings_score_check CHECK (score BETWEEN 1 AND 5)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_service_ratings_created ON service_ratings(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_service_ratings_agent ON service_ratings(assigned_agent_id) WHERE assigned_agent_id IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS audit_events (
			id TEXT PRIMARY KEY,
			actor_kind TEXT NOT NULL,
			actor_id TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			resource TEXT NOT NULL DEFAULT '',
			resource_id TEXT NOT NULL DEFAULT '',
			ip_address TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT audit_events_actor_kind_check CHECK (actor_kind IN ('admin', 'agent', 'visitor'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_created ON audit_events(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_actor ON audit_events(actor_kind, actor_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_resource ON audit_events(resource, resource_id, created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.pool.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func scanAdmin(row scanTarget) (domain.AdminUser, error) {
	var out domain.AdminUser
	err := row.Scan(&out.ID, &out.Account, &out.Name, &out.Password, &out.Created)
	return out, err
}

func scanAgent(row scanTarget) (domain.Agent, error) {
	var out domain.Agent
	var disabled sql.NullTime
	err := row.Scan(
		&out.ID,
		&out.Account,
		&out.Name,
		&out.Group,
		&out.Password,
		&out.Status,
		&out.MaxConversations,
		&out.CurrentConversations,
		&disabled,
		&out.Created,
		&out.Updated,
	)
	if disabled.Valid {
		out.DisabledAt = &disabled.Time
	}
	return out, err
}

func scanAgents(rows pgx.Rows) ([]domain.Agent, error) {
	result := []domain.Agent{}
	for rows.Next() {
		agent, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, agent)
	}
	return result, rows.Err()
}

func scanConversation(row scanTarget) (domain.Conversation, error) {
	var out domain.Conversation
	var remarkUpdatedAt sql.NullTime
	var lastMessageAt sql.NullTime
	err := row.Scan(
		&out.ID,
		&out.VisitorID,
		&out.VisitorIP,
		&out.VisitorRemark,
		&out.RemarkUpdatedBy,
		&remarkUpdatedAt,
		&out.Status,
		&out.AssignedAgentID,
		&out.Source,
		&out.LastMessage,
		&lastMessageAt,
		&out.UnreadForAgent,
		&out.UnreadForVisitor,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if remarkUpdatedAt.Valid {
		out.RemarkUpdatedAt = &remarkUpdatedAt.Time
	}
	if lastMessageAt.Valid {
		out.LastMessageAt = &lastMessageAt.Time
	}
	return out, err
}

func scanConversations(rows pgx.Rows) ([]domain.Conversation, error) {
	result := []domain.Conversation{}
	for rows.Next() {
		conversation, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, conversation)
	}
	return result, rows.Err()
}

func scanMessage(row scanTarget) (domain.Message, error) {
	var out domain.Message
	var revokedAt sql.NullTime
	var revokedByKind sql.NullString
	var revokedByID sql.NullString
	err := row.Scan(&out.ServerMsgID, &out.ClientMsgID, &out.ConversationID, &out.SenderType, &out.SenderID, &out.MessageType, &out.Content, &out.CreatedAt, &revokedAt, &revokedByKind, &revokedByID)
	if revokedAt.Valid {
		out.RevokedAt = &revokedAt.Time
	}
	if revokedByKind.Valid {
		out.RevokedByKind = domain.AccountKind(revokedByKind.String)
	}
	if revokedByID.Valid {
		out.RevokedByID = revokedByID.String
	}
	return out, err
}

func scanMessages(rows pgx.Rows) ([]domain.Message, error) {
	result := []domain.Message{}
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, msg)
	}
	return result, rows.Err()
}

func scanRating(row scanTarget) (domain.ServiceRating, error) {
	var out domain.ServiceRating
	err := row.Scan(
		&out.ID,
		&out.ConversationID,
		&out.VisitorID,
		&out.AssignedAgentID,
		&out.Score,
		&out.Tags,
		&out.Comment,
		&out.CreatedAt,
	)
	return out, err
}

func scanRatings(rows pgx.Rows) ([]domain.ServiceRating, error) {
	result := []domain.ServiceRating{}
	for rows.Next() {
		rating, err := scanRating(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, rating)
	}
	return result, rows.Err()
}

func scanAuditEvent(row scanTarget) (domain.AuditEvent, error) {
	var out domain.AuditEvent
	err := row.Scan(
		&out.ID,
		&out.ActorKind,
		&out.ActorID,
		&out.Action,
		&out.Resource,
		&out.ResourceID,
		&out.IPAddress,
		&out.UserAgent,
		&out.Description,
		&out.CreatedAt,
	)
	return out, err
}

func scanAuditEvents(rows pgx.Rows) ([]domain.AuditEvent, error) {
	result := []domain.AuditEvent{}
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func scanPushDevice(row scanTarget) (domain.PushDevice, error) {
	var out domain.PushDevice
	err := row.Scan(&out.ID, &out.AgentID, &out.Platform, &out.Token, &out.Provider, &out.Enabled, &out.UpdatedAt, &out.CreatedAt)
	return out, err
}

func dbContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultDBTimeout)
}

func dbNow() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func mapNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func ipForDB(value string) string {
	value = normalizeIP(value)
	if net.ParseIP(value) == nil {
		return "0.0.0.0"
	}
	return value
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isBusinessTime(business domain.BusinessHours, now time.Time) bool {
	location, err := time.LoadLocation(business.Timezone)
	if err != nil {
		location = time.FixedZone("CST", 8*3600)
	}
	local := now.In(location)
	start, err1 := time.Parse("15:04", business.Start)
	end, err2 := time.Parse("15:04", business.End)
	if err1 != nil || err2 != nil {
		return true
	}
	currentMinutes := local.Hour()*60 + local.Minute()
	startMinutes := start.Hour()*60 + start.Minute()
	endMinutes := end.Hour()*60 + end.Minute()
	if startMinutes <= endMinutes {
		return currentMinutes >= startMinutes && currentMinutes < endMinutes
	}
	return currentMinutes >= startMinutes || currentMinutes < endMinutes
}
