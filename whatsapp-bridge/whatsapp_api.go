package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Types for parameters
// SearchContactsParams represents parameters for searching contacts
type SearchContactsParams struct {
	Query string `json:"query"`
}

// ListMessagesParams represents parameters for listing messages
type ListMessagesParams struct {
	After           *time.Time `json:"after,omitempty"`
	Before          *time.Time `json:"before,omitempty"`
	SenderPhoneNumber string   `json:"sender_phone_number,omitempty"`
	ChatJID         string     `json:"chat_jid,omitempty"`
	Query           string     `json:"query,omitempty"`
	Limit           int        `json:"limit"`
	Page            int        `json:"page"`
	IncludeContext  bool       `json:"include_context"`
	ContextBefore   int        `json:"context_before"`
	ContextAfter    int        `json:"context_after"`
}

// ListChatsParams represents parameters for listing chats
type ListChatsParams struct {
	Query             string `json:"query,omitempty"`
	Limit             int    `json:"limit"`
	Page              int    `json:"page"`
	IncludeLastMessage bool   `json:"include_last_message"`
	SortBy            string `json:"sort_by"`
}

// MessageContextParams represents parameters for getting message context
type MessageContextParams struct {
	MessageID string `json:"message_id"`
	Before    int    `json:"before"`
	After     int    `json:"after"`
}

// Result structs
// ChatResult represents a chat with its metadata
type ChatResult struct {
	JID           string    `json:"jid"`
	Name          string    `json:"name"`
	LastMessageAt time.Time `json:"last_message_at"`
	LastMessage   *Message  `json:"last_message,omitempty"`
}

// MessageResult represents a message with its metadata
type MessageResult struct {
	ID           string    `json:"id"`
	ChatJID      string    `json:"chat_jid"`
	Sender       string    `json:"sender"`
	SenderName   string    `json:"sender_name"`
	Content      string    `json:"content"`
	Timestamp    time.Time `json:"timestamp"`
	IsFromMe     bool      `json:"is_from_me"`
	MediaType    string    `json:"media_type,omitempty"`
	MediaPath    string    `json:"media_path,omitempty"`
	ContextItems []Message `json:"context_items,omitempty"`
}

// SearchContactsResult represents a contact with its metadata
type SearchContactsResult struct {
	JID         string `json:"jid"`
	Name        string `json:"name"`
	PhoneNumber string `json:"phone_number"`
}

// DBHandler handles database operations
type DBHandler struct {
	db *sql.DB
}

// NewDBHandler creates a new database handler
func NewDBHandler(db *sql.DB) *DBHandler {
	return &DBHandler{
		db: db,
	}
}

// SearchContacts searches for contacts matching the query
func (h *DBHandler) SearchContacts(params SearchContactsParams) ([]SearchContactsResult, error) {
	query := strings.TrimSpace(params.Query)
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	// Search by name or phone number (which is the JID user part)
	rows, err := h.db.Query(`
		SELECT DISTINCT jid, name 
		FROM chats 
		WHERE jid LIKE ? OR name LIKE ?
		ORDER BY name
	`, "%"+query+"%", "%"+query+"%")
	
	if err != nil {
		return nil, fmt.Errorf("failed to search contacts: %v", err)
	}
	defer rows.Close()

	var results []SearchContactsResult
	for rows.Next() {
		var contact SearchContactsResult
		err := rows.Scan(&contact.JID, &contact.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to scan contact row: %v", err)
		}

		// Extract phone number from JID
		if strings.Contains(contact.JID, "@s.whatsapp.net") {
			contact.PhoneNumber = strings.Split(contact.JID, "@")[0]
		}

		results = append(results, contact)
	}

	return results, nil
}

// ListMessages lists messages matching the specified criteria
func (h *DBHandler) ListMessages(params ListMessagesParams) ([]MessageResult, error) {
	// Build the WHERE clause based on the parameters
	whereClause := []string{}
	args := []interface{}{}

	if params.After != nil {
		whereClause = append(whereClause, "timestamp > ?")
		args = append(args, params.After)
	}

	if params.Before != nil {
		whereClause = append(whereClause, "timestamp < ?")
		args = append(args, params.Before)
	}

	if params.SenderPhoneNumber != "" {
		// Convert phone number to JID format
		senderJID := params.SenderPhoneNumber + "@s.whatsapp.net"
		whereClause = append(whereClause, "sender = ?")
		args = append(args, senderJID)
	}

	if params.ChatJID != "" {
		whereClause = append(whereClause, "chat_jid = ?")
		args = append(args, params.ChatJID)
	}

	if params.Query != "" {
		whereClause = append(whereClause, "content LIKE ?")
		args = append(args, "%"+params.Query+"%")
	}

	// Build the full query
	query := `
		SELECT m.id, m.chat_jid, m.sender, c.name as chat_name, m.content, m.timestamp, m.is_from_me, 
		       m.media_type, m.filename
		FROM messages m
		JOIN chats c ON m.chat_jid = c.jid
	`

	if len(whereClause) > 0 {
		query += " WHERE " + strings.Join(whereClause, " AND ")
	}

	query += " ORDER BY m.timestamp DESC LIMIT ? OFFSET ?"
	args = append(args, params.Limit, params.Page*params.Limit)

	// Execute the query
	rows, err := h.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %v", err)
	}
	defer rows.Close()

	var results []MessageResult
	for rows.Next() {
		var msg MessageResult
		var filename sql.NullString
		err := rows.Scan(
			&msg.ID, &msg.ChatJID, &msg.Sender, &msg.SenderName, &msg.Content, 
			&msg.Timestamp, &msg.IsFromMe, &msg.MediaType, &filename,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message row: %v", err)
		}

		if filename.Valid && filename.String != "" {
			// Create a path to the file (this is safe for API consumption)
			msg.MediaPath = fmt.Sprintf("store/%s/%s", 
				strings.ReplaceAll(msg.ChatJID, ":", "_"), 
				filename.String)
		}

		// If context is requested, get it
		if params.IncludeContext {
			contextItems, err := h.getMessageContext(msg.ID, msg.ChatJID, params.ContextBefore, params.ContextAfter)
			if err != nil {
				return nil, fmt.Errorf("failed to get message context: %v", err)
			}
			for _, item := range contextItems {
				msg.ContextItems = append(msg.ContextItems, item)
			}
		}

		results = append(results, msg)
	}

	return results, nil
}

// getMessageContext gets context messages around a specific message
func (h *DBHandler) getMessageContext(messageID, chatJID string, before, after int) ([]Message, error) {
	// First, get the timestamp of the target message
	var timestamp time.Time
	err := h.db.QueryRow(
		"SELECT timestamp FROM messages WHERE id = ? AND chat_jid = ?",
		messageID, chatJID,
	).Scan(&timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to get target message timestamp: %v", err)
	}

	// Get messages before the target
	beforeQuery := `
		SELECT sender, content, timestamp, is_from_me, media_type, filename
		FROM messages 
		WHERE chat_jid = ? AND timestamp < ?
		ORDER BY timestamp DESC
		LIMIT ?
	`
	beforeRows, err := h.db.Query(beforeQuery, chatJID, timestamp, before)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages before target: %v", err)
	}
	defer beforeRows.Close()

	var beforeMessages []Message
	for beforeRows.Next() {
		var msg Message
		var mediaType, filename sql.NullString
		err := beforeRows.Scan(&msg.Sender, &msg.Content, &msg.Time, &msg.IsFromMe, &mediaType, &filename)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message row: %v", err)
		}
		
		if mediaType.Valid {
			msg.MediaType = mediaType.String
		}
		if filename.Valid {
			msg.Filename = filename.String
		}
		
		beforeMessages = append(beforeMessages, msg)
	}

	// Reverse the before messages to get them in chronological order
	for i, j := 0, len(beforeMessages)-1; i < j; i, j = i+1, j-1 {
		beforeMessages[i], beforeMessages[j] = beforeMessages[j], beforeMessages[i]
	}

	// Get messages after the target
	afterQuery := `
		SELECT sender, content, timestamp, is_from_me, media_type, filename
		FROM messages 
		WHERE chat_jid = ? AND timestamp > ?
		ORDER BY timestamp ASC
		LIMIT ?
	`
	afterRows, err := h.db.Query(afterQuery, chatJID, timestamp, after)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages after target: %v", err)
	}
	defer afterRows.Close()

	var afterMessages []Message
	for afterRows.Next() {
		var msg Message
		var mediaType, filename sql.NullString
		err := beforeRows.Scan(&msg.Sender, &msg.Content, &msg.Time, &msg.IsFromMe, &mediaType, &filename)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message row: %v", err)
		}
		
		if mediaType.Valid {
			msg.MediaType = mediaType.String
		}
		if filename.Valid {
			msg.Filename = filename.String
		}
		
		afterMessages = append(afterMessages, msg)
	}

	// Combine the before, target, and after messages
	allMessages := append(beforeMessages, afterMessages...)
	return allMessages, nil
}

// GetMessageContext gets context around a specific message
func (h *DBHandler) GetMessageContext(params MessageContextParams) (*MessageResult, error) {
	// First, get the target message
	targetQuery := `
		SELECT m.id, m.chat_jid, m.sender, c.name as chat_name, m.content, m.timestamp, m.is_from_me, 
		       m.media_type, m.filename
		FROM messages m
		JOIN chats c ON m.chat_jid = c.jid
		WHERE m.id = ?
	`
	
	var msg MessageResult
	var chatJID string
	var filename, mediaType sql.NullString
	
	err := h.db.QueryRow(targetQuery, params.MessageID).Scan(
		&msg.ID, &chatJID, &msg.Sender, &msg.SenderName, &msg.Content, 
		&msg.Timestamp, &msg.IsFromMe, &mediaType, &filename,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get target message: %v", err)
	}
	
	if mediaType.Valid {
		msg.MediaType = mediaType.String
	}
	
	if filename.Valid && filename.String != "" {
		// Create a path to the file (this is safe for API consumption)
		msg.MediaPath = fmt.Sprintf("store/%s/%s", 
			strings.ReplaceAll(chatJID, ":", "_"), 
			filename.String)
	}

	// Get context messages
	contextItems, err := h.getMessageContext(msg.ID, chatJID, params.Before, params.After)
	if err != nil {
		return nil, fmt.Errorf("failed to get message context: %v", err)
	}
	msg.ContextItems = contextItems

	return &msg, nil
}

// ListChats lists chats matching the specified criteria
func (h *DBHandler) ListChats(params ListChatsParams) ([]ChatResult, error) {
	// Build the WHERE clause based on the parameters
	whereClause := []string{}
	args := []interface{}{}

	if params.Query != "" {
		whereClause = append(whereClause, "(jid LIKE ? OR name LIKE ?)")
		args = append(args, "%"+params.Query+"%", "%"+params.Query+"%")
	}

	// Build the ORDER BY clause based on sort_by
	orderBy := "last_message_time DESC"
	if params.SortBy == "name" {
		orderBy = "name ASC"
	}

	// Build the full query
	query := "SELECT jid, name, last_message_time FROM chats"
	if len(whereClause) > 0 {
		query += " WHERE " + strings.Join(whereClause, " AND ")
	}
	query += " ORDER BY " + orderBy + " LIMIT ? OFFSET ?"
	args = append(args, params.Limit, params.Page*params.Limit)

	// Execute the query
	rows, err := h.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list chats: %v", err)
	}
	defer rows.Close()

	var results []ChatResult
	for rows.Next() {
		var chat ChatResult
		err := rows.Scan(&chat.JID, &chat.Name, &chat.LastMessageAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chat row: %v", err)
		}

		// If last message is requested, get it
		if params.IncludeLastMessage {
			lastMsg, err := h.getLastMessage(chat.JID)
			if err != nil {
				// Don't fail the whole request if one last message fails
				fmt.Printf("Warning: failed to get last message for chat %s: %v\n", chat.JID, err)
			} else if lastMsg != nil {
				chat.LastMessage = lastMsg
			}
		}

		results = append(results, chat)
	}

	return results, nil
}

// getLastMessage gets the last message for a chat
func (h *DBHandler) getLastMessage(chatJID string) (*Message, error) {
	query := `
		SELECT sender, content, timestamp, is_from_me, media_type, filename
		FROM messages
		WHERE chat_jid = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`
	
	var msg Message
	var mediaType, filename sql.NullString
	
	err := h.db.QueryRow(query, chatJID).Scan(
		&msg.Sender, &msg.Content, &msg.Time, &msg.IsFromMe, &mediaType, &filename,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No last message found, not an error
		}
		return nil, fmt.Errorf("failed to get last message: %v", err)
	}
	
	if mediaType.Valid {
		msg.MediaType = mediaType.String
	}
	
	if filename.Valid {
		msg.Filename = filename.String
	}
	
	return &msg, nil
}

// GetChat gets a chat by JID
func (h *DBHandler) GetChat(chatJID string, includeLastMessage bool) (*ChatResult, error) {
	query := "SELECT jid, name, last_message_time FROM chats WHERE jid = ?"
	
	var chat ChatResult
	err := h.db.QueryRow(query, chatJID).Scan(&chat.JID, &chat.Name, &chat.LastMessageAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get chat: %v", err)
	}

	// If last message is requested, get it
	if includeLastMessage {
		lastMsg, err := h.getLastMessage(chat.JID)
		if err != nil {
			fmt.Printf("Warning: failed to get last message for chat %s: %v\n", chat.JID, err)
		} else if lastMsg != nil {
			chat.LastMessage = lastMsg
		}
	}

	return &chat, nil
}

// GetDirectChatByContact gets a direct chat by contact phone number
func (h *DBHandler) GetDirectChatByContact(phoneNumber string) (*ChatResult, error) {
	// Form the JID from the phone number
	jid := phoneNumber + "@s.whatsapp.net"
	
	return h.GetChat(jid, true)
}

// GetContactChats gets all chats involving a contact
func (h *DBHandler) GetContactChats(jid string, limit, page int) ([]ChatResult, error) {
	// This implementation is simplified - in a real world scenario,
	// we'd need to search messages to find all chats where this contact participates
	params := ListChatsParams{
		Query:             jid,
		Limit:             limit,
		Page:              page,
		IncludeLastMessage: true,
		SortBy:            "last_active",
	}
	
	return h.ListChats(params)
}

// GetLastInteraction gets the most recent message involving a contact
func (h *DBHandler) GetLastInteraction(jid string) (*MessageResult, error) {
	query := `
		SELECT m.id, m.chat_jid, m.sender, c.name as chat_name, m.content, m.timestamp, m.is_from_me, 
		       m.media_type, m.filename
		FROM messages m
		JOIN chats c ON m.chat_jid = c.jid
		WHERE m.sender = ? OR m.chat_jid = ?
		ORDER BY m.timestamp DESC
		LIMIT 1
	`
	
	var msg MessageResult
	var mediaType, filename sql.NullString
	
	err := h.db.QueryRow(query, jid, jid).Scan(
		&msg.ID, &msg.ChatJID, &msg.Sender, &msg.SenderName, &msg.Content, 
		&msg.Timestamp, &msg.IsFromMe, &mediaType, &filename,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get last interaction: %v", err)
	}
	
	if mediaType.Valid {
		msg.MediaType = mediaType.String
	}
	
	if filename.Valid && filename.String != "" {
		// Create a path to the file (this is safe for API consumption)
		msg.MediaPath = fmt.Sprintf("store/%s/%s", 
			strings.ReplaceAll(msg.ChatJID, ":", "_"), 
			filename.String)
	}
	
	return &msg, nil
} 