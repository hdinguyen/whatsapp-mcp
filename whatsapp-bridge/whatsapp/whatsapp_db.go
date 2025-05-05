package whatsapp

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// WhatsApp represents a WhatsApp client
type WhatsApp struct {
	MessagesDBPath string
	db             *sql.DB
}

// NewWhatsApp creates a new WhatsApp client with the specified database path
func NewWhatsApp(dbPath string) (*WhatsApp, error) {
	if dbPath == "" {
		// Default path if none provided
		dbPath = filepath.Join(filepath.Dir(filepath.Dir(filepath.Join("."))), "whatsapp-bridge", "store", "messages.db")
	}
	
	// Initialize database connection
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}
	
	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %v", err)
	}
	
	return &WhatsApp{
		MessagesDBPath: dbPath,
		db:             db,
	}, nil
}

// Close closes the database connection
func (wa *WhatsApp) Close() error {
	if wa.db != nil {
		return wa.db.Close()
	}
	return nil
}

// Message represents a WhatsApp message
type Message struct {
	Timestamp  time.Time
	Sender     string
	Content    string
	IsFromMe   bool
	ChatJID    string
	ID         string
	ChatName   string
	MediaType  string
}

// Chat represents a WhatsApp chat
type Chat struct {
	JID            string
	Name           string
	LastMessageTime time.Time
	LastMessage    string
	LastSender     string
	LastIsFromMe   bool
}

// Contact represents a WhatsApp contact
type Contact struct {
	PhoneNumber string
	Name        string
	JID         string
}

// MessageContext represents messages around a specific message
type MessageContext struct {
	Message Message
	Before  []Message
	After   []Message
}

// IsGroup determines if a chat is a group based on JID pattern
func (c *Chat) IsGroup() bool {
	return strings.HasSuffix(c.JID, "@g.us")
}

// GetSenderName retrieves the name of a sender from their JID
func (wa *WhatsApp) GetSenderName(senderJID string) string {
	// First try matching by exact JID
	var name string
	err := wa.db.QueryRow(`
		SELECT name
		FROM chats
		WHERE jid = ?
		LIMIT 1
	`, senderJID).Scan(&name)

	// If no result, try looking for the number within JIDs
	if err != nil || name == "" {
		// Extract the phone number part if it's a JID
		phonePart := senderJID
		if strings.Contains(senderJID, "@") {
			phonePart = strings.Split(senderJID, "@")[0]
		}

		err = wa.db.QueryRow(`
			SELECT name
			FROM chats
			WHERE jid LIKE ?
			LIMIT 1
		`, "%"+phonePart+"%").Scan(&name)
	}

	if err == nil && name != "" {
		return name
	}

	return senderJID
}

// FormatMessage formats a single message with consistent formatting
func (wa *WhatsApp) FormatMessage(message Message, showChatInfo bool) string {
	output := ""

	if showChatInfo && message.ChatName != "" {
		output += fmt.Sprintf("[%s] Chat: %s ", message.Timestamp.Format("2006-01-02 15:04:05"), message.ChatName)
	} else {
		output += fmt.Sprintf("[%s] ", message.Timestamp.Format("2006-01-02 15:04:05"))
	}

	contentPrefix := ""
	if message.MediaType != "" {
		contentPrefix = fmt.Sprintf("[%s - Message ID: %s - Chat JID: %s] ", message.MediaType, message.ID, message.ChatJID)
	}

	senderName := "Me"
	if !message.IsFromMe {
		senderName = wa.GetSenderName(message.Sender)
	}

	output += fmt.Sprintf("From: %s: %s%s\n", senderName, contentPrefix, message.Content)
	return output
}

// FormatMessagesList formats a list of messages
func (wa *WhatsApp) FormatMessagesList(messages []Message, showChatInfo bool) string {
	if len(messages) == 0 {
		return "No messages to display."
	}

	var output strings.Builder
	for _, message := range messages {
		output.WriteString(wa.FormatMessage(message, showChatInfo))
	}
	return output.String()
}

// ListMessages gets messages matching the specified criteria with optional context
func (wa *WhatsApp) ListMessages(
	after string,
	before string,
	senderPhoneNumber string,
	chatJID string,
	query string,
	limit int,
	page int,
	includeContext bool,
	contextBefore int,
	contextAfter int,
) string {
	// Build base query
	queryParts := []string{
		"SELECT messages.timestamp, messages.sender, chats.name, messages.content, messages.is_from_me, chats.jid, messages.id, messages.media_type FROM messages",
		"JOIN chats ON messages.chat_jid = chats.jid",
	}
	whereClauses := []string{}
	params := []interface{}{}

	// Add filters
	if after != "" {
		afterTime, err := time.Parse(time.RFC3339, after)
		if err != nil {
			return fmt.Sprintf("Invalid date format for 'after': %s. Please use ISO-8601 format.", after)
		}
		whereClauses = append(whereClauses, "messages.timestamp > ?")
		params = append(params, afterTime.Format("2006-01-02 15:04:05"))
	}

	if before != "" {
		beforeTime, err := time.Parse(time.RFC3339, before)
		if err != nil {
			return fmt.Sprintf("Invalid date format for 'before': %s. Please use ISO-8601 format.", before)
		}
		whereClauses = append(whereClauses, "messages.timestamp < ?")
		params = append(params, beforeTime.Format("2006-01-02 15:04:05"))
	}

	if senderPhoneNumber != "" {
		whereClauses = append(whereClauses, "messages.sender = ?")
		params = append(params, senderPhoneNumber)
	}

	if chatJID != "" {
		whereClauses = append(whereClauses, "messages.chat_jid = ?")
		params = append(params, chatJID)
	}

	if query != "" {
		whereClauses = append(whereClauses, "LOWER(messages.content) LIKE LOWER(?)")
		params = append(params, "%"+query+"%")
	}

	if len(whereClauses) > 0 {
		queryParts = append(queryParts, "WHERE "+strings.Join(whereClauses, " AND "))
	}

	// Add pagination
	offset := page * limit
	queryParts = append(queryParts, "ORDER BY messages.timestamp DESC")
	queryParts = append(queryParts, "LIMIT ? OFFSET ?")
	params = append(params, limit, offset)

	// Execute the query
	rows, err := wa.db.Query(strings.Join(queryParts, " "), params...)
	if err != nil {
		fmt.Printf("Database error: %v\n", err)
		return ""
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		var msg Message
		var timestampStr string
		var isFromMe bool
		err := rows.Scan(
			&timestampStr,
			&msg.Sender,
			&msg.ChatName,
			&msg.Content,
			&isFromMe,
			&msg.ChatJID,
			&msg.ID,
			&msg.MediaType,
		)
		if err != nil {
			fmt.Printf("Error scanning row: %v\n", err)
			continue
		}

		msg.Timestamp, _ = time.Parse("2006-01-02 15:04:05", timestampStr)
		msg.IsFromMe = isFromMe
		messages = append(messages, msg)
	}

	if includeContext && len(messages) > 0 {
		// Add context for each message
		messagesWithContext := []Message{}
		for _, msg := range messages {
			context, err := wa.GetMessageContext(msg.ID, contextBefore, contextAfter)
			if err != nil {
				fmt.Printf("Error getting context: %v\n", err)
				continue
			}
			messagesWithContext = append(messagesWithContext, context.Before...)
			messagesWithContext = append(messagesWithContext, context.Message)
			messagesWithContext = append(messagesWithContext, context.After...)
		}

		return wa.FormatMessagesList(messagesWithContext, true)
	}

	// Format and display messages without context
	return wa.FormatMessagesList(messages, true)
}

// GetMessageContext gets context around a specific message
func (wa *WhatsApp) GetMessageContext(messageID string, before int, after int) (MessageContext, error) {
	// Get the target message first
	var targetMessage Message
	var timestampStr string
	var isFromMe bool
	var chatJID string

	err := wa.db.QueryRow(`
		SELECT messages.timestamp, messages.sender, chats.name, messages.content, messages.is_from_me, chats.jid, messages.id, messages.chat_jid, messages.media_type
		FROM messages
		JOIN chats ON messages.chat_jid = chats.jid
		WHERE messages.id = ?
	`, messageID).Scan(
		&timestampStr,
		&targetMessage.Sender,
		&targetMessage.ChatName,
		&targetMessage.Content,
		&isFromMe,
		&targetMessage.ChatJID,
		&targetMessage.ID,
		&chatJID,
		&targetMessage.MediaType,
	)

	if err != nil {
		return MessageContext{}, fmt.Errorf("message with ID %s not found: %v", messageID, err)
	}

	targetMessage.Timestamp, _ = time.Parse("2006-01-02 15:04:05", timestampStr)
	targetMessage.IsFromMe = isFromMe

	// Get messages before
	beforeMessages := []Message{}
	rowsBefore, err := wa.db.Query(`
		SELECT messages.timestamp, messages.sender, chats.name, messages.content, messages.is_from_me, chats.jid, messages.id, messages.media_type
		FROM messages
		JOIN chats ON messages.chat_jid = chats.jid
		WHERE messages.chat_jid = ? AND messages.timestamp < ?
		ORDER BY messages.timestamp DESC
		LIMIT ?
	`, chatJID, timestampStr, before)

	if err == nil {
		defer rowsBefore.Close()
		for rowsBefore.Next() {
			var msg Message
			var msgTimestampStr string
			var msgIsFromMe bool
			err := rowsBefore.Scan(
				&msgTimestampStr,
				&msg.Sender,
				&msg.ChatName,
				&msg.Content,
				&msgIsFromMe,
				&msg.ChatJID,
				&msg.ID,
				&msg.MediaType,
			)
			if err != nil {
				fmt.Printf("Error scanning row: %v\n", err)
				continue
			}

			msg.Timestamp, _ = time.Parse("2006-01-02 15:04:05", msgTimestampStr)
			msg.IsFromMe = msgIsFromMe
			beforeMessages = append(beforeMessages, msg)
		}
	}

	// Get messages after
	afterMessages := []Message{}
	rowsAfter, err := wa.db.Query(`
		SELECT messages.timestamp, messages.sender, chats.name, messages.content, messages.is_from_me, chats.jid, messages.id, messages.media_type
		FROM messages
		JOIN chats ON messages.chat_jid = chats.jid
		WHERE messages.chat_jid = ? AND messages.timestamp > ?
		ORDER BY messages.timestamp ASC
		LIMIT ?
	`, chatJID, timestampStr, after)

	if err == nil {
		defer rowsAfter.Close()
		for rowsAfter.Next() {
			var msg Message
			var msgTimestampStr string
			var msgIsFromMe bool
			err := rowsAfter.Scan(
				&msgTimestampStr,
				&msg.Sender,
				&msg.ChatName,
				&msg.Content,
				&msgIsFromMe,
				&msg.ChatJID,
				&msg.ID,
				&msg.MediaType,
			)
			if err != nil {
				fmt.Printf("Error scanning row: %v\n", err)
				continue
			}

			msg.Timestamp, _ = time.Parse("2006-01-02 15:04:05", msgTimestampStr)
			msg.IsFromMe = msgIsFromMe
			afterMessages = append(afterMessages, msg)
		}
	}

	return MessageContext{
		Message: targetMessage,
		Before:  beforeMessages,
		After:   afterMessages,
	}, nil
}

// ListChats gets chats matching the specified criteria
func (wa *WhatsApp) ListChats(
	query string,
	limit int,
	page int,
	includeLastMessage bool,
	sortBy string,
) ([]Chat, error) {
	// Build base query
	queryParts := []string{`
		SELECT 
			chats.jid,
			chats.name,
			chats.last_message_time,
			messages.content as last_message,
			messages.sender as last_sender,
			messages.is_from_me as last_is_from_me
		FROM chats
	`}

	if includeLastMessage {
		queryParts = append(queryParts, `
			LEFT JOIN messages ON chats.jid = messages.chat_jid 
			AND chats.last_message_time = messages.timestamp
		`)
	}

	whereClauses := []string{}
	params := []interface{}{}

	if query != "" {
		whereClauses = append(whereClauses, "(LOWER(chats.name) LIKE LOWER(?) OR chats.jid LIKE ?)")
		params = append(params, "%"+query+"%", "%"+query+"%")
	}

	if len(whereClauses) > 0 {
		queryParts = append(queryParts, "WHERE "+strings.Join(whereClauses, " AND "))
	}

	// Add sorting
	orderBy := "chats.last_message_time DESC"
	if sortBy == "name" {
		orderBy = "chats.name"
	}
	queryParts = append(queryParts, fmt.Sprintf("ORDER BY %s", orderBy))

	// Add pagination
	offset := page * limit
	queryParts = append(queryParts, "LIMIT ? OFFSET ?")
	params = append(params, limit, offset)

	debugQuery := strings.Join(queryParts, " ")
	fmt.Println(debugQuery)
	// Execute the query
	rows, err := wa.db.Query(strings.Join(queryParts, " "), params...)
	if err != nil {
		return nil, fmt.Errorf("database error: %v", err)
	}
	defer rows.Close()

	chats := []Chat{}
	for rows.Next() {
		var chat Chat
		var lastMessageTimeStr sql.NullString
		var lastMessage sql.NullString
		var lastSender sql.NullString
		var lastIsFromMe sql.NullInt64
		var name sql.NullString

		err := rows.Scan(
			&chat.JID,
			&name,
			&lastMessageTimeStr,
			&lastMessage,
			&lastSender,
			&lastIsFromMe,
		)

		if err != nil {
			fmt.Printf("Error scanning row: %v\n", err)
			continue
		}

		if name.Valid {
			chat.Name = name.String
		}

		if lastMessageTimeStr.Valid {
			chat.LastMessageTime, _ = time.Parse("2006-01-02 15:04:05", lastMessageTimeStr.String)
		}

		if lastMessage.Valid {
			chat.LastMessage = lastMessage.String
		}

		if lastSender.Valid {
			chat.LastSender = lastSender.String
		}

		if lastIsFromMe.Valid {
			chat.LastIsFromMe = lastIsFromMe.Int64 != 0
		}

		chats = append(chats, chat)
	}

	return chats, nil
}

// SearchContacts searches contacts by name or phone number
func (wa *WhatsApp) SearchContacts(query string) ([]Contact, error) {
	// Split query into characters to support partial matching
	searchPattern := "%" + query + "%"

	rows, err := wa.db.Query(`
		SELECT DISTINCT 
			jid,
			name
		FROM chats
		WHERE 
			(LOWER(name) LIKE LOWER(?) OR LOWER(jid) LIKE LOWER(?))
			AND jid NOT LIKE '%@g.us'
		ORDER BY name, jid
		LIMIT 50
	`, searchPattern, searchPattern)

	if err != nil {
		return nil, fmt.Errorf("database error: %v", err)
	}
	defer rows.Close()

	contacts := []Contact{}
	for rows.Next() {
		var contact Contact
		var jid string
		var name sql.NullString

		err := rows.Scan(&jid, &name)
		if err != nil {
			fmt.Printf("Error scanning row: %v\n", err)
			continue
		}

		contact.JID = jid
		if name.Valid {
			contact.Name = name.String
		}

		// Extract phone number from JID
		parts := strings.Split(jid, "@")
		if len(parts) > 0 {
			contact.PhoneNumber = parts[0]
		}

		contacts = append(contacts, contact)
	}

	return contacts, nil
}

// GetContactChats gets all chats involving the contact
func (wa *WhatsApp) GetContactChats(jid string, limit int, page int) ([]Chat, error) {
	rows, err := wa.db.Query(`
		SELECT DISTINCT
			c.jid,
			c.name,
			c.last_message_time,
			m.content as last_message,
			m.sender as last_sender,
			m.is_from_me as last_is_from_me
		FROM chats c
		JOIN messages m ON c.jid = m.chat_jid
		WHERE m.sender = ? OR c.jid = ?
		ORDER BY c.last_message_time DESC
		LIMIT ? OFFSET ?
	`, jid, jid, limit, page*limit)

	if err != nil {
		return nil, fmt.Errorf("database error: %v", err)
	}
	defer rows.Close()

	chats := []Chat{}
	for rows.Next() {
		var chat Chat
		var lastMessageTimeStr sql.NullString
		var lastMessage sql.NullString
		var lastSender sql.NullString
		var lastIsFromMe sql.NullBool
		var name sql.NullString

		err := rows.Scan(
			&chat.JID,
			&name,
			&lastMessageTimeStr,
			&lastMessage,
			&lastSender,
			&lastIsFromMe,
		)

		if err != nil {
			fmt.Printf("Error scanning row: %v\n", err)
			continue
		}

		if name.Valid {
			chat.Name = name.String
		}

		if lastMessageTimeStr.Valid {
			chat.LastMessageTime, _ = time.Parse("2006-01-02 15:04:05", lastMessageTimeStr.String)
		}

		if lastMessage.Valid {
			chat.LastMessage = lastMessage.String
		}

		if lastSender.Valid {
			chat.LastSender = lastSender.String
		}

		if lastIsFromMe.Valid {
			chat.LastIsFromMe = lastIsFromMe.Bool != false
		}

		chats = append(chats, chat)
	}

	return chats, nil
}

// GetLastInteraction gets most recent message involving the contact
func (wa *WhatsApp) GetLastInteraction(jid string) string {
	var msg Message
	var timestampStr string
	var isFromMe bool

	err := wa.db.QueryRow(`
		SELECT 
			m.timestamp,
			m.sender,
			c.name,
			m.content,
			m.is_from_me,
			c.jid,
			m.id,
			m.media_type
		FROM messages m
		JOIN chats c ON m.chat_jid = c.jid
		WHERE m.sender = ? OR c.jid = ?
		ORDER BY m.timestamp DESC
		LIMIT 1
	`, jid, jid).Scan(
		&timestampStr,
		&msg.Sender,
		&msg.ChatName,
		&msg.Content,
		&isFromMe,
		&msg.ChatJID,
		&msg.ID,
		&msg.MediaType,
	)

	if err != nil {
		return ""
	}

	msg.Timestamp, _ = time.Parse("2006-01-02 15:04:05", timestampStr)
	msg.IsFromMe = isFromMe

	return wa.FormatMessage(msg, true)
}

// GetChat gets chat metadata by JID
func (wa *WhatsApp) GetChat(chatJID string, includeLastMessage bool) (*Chat, error) {
	query := `
		SELECT 
			c.jid,
			c.name,
			c.last_message_time
	`

	if includeLastMessage {
		query += `,
			m.content as last_message,
			m.sender as last_sender,
			m.is_from_me as last_is_from_me
		`
	} else {
		query += `,
			NULL as last_message,
			NULL as last_sender,
			NULL as last_is_from_me
		`
	}

	query += `
		FROM chats c
	`

	if includeLastMessage {
		query += `
			LEFT JOIN messages m ON c.jid = m.chat_jid 
			AND c.last_message_time = m.timestamp
		`
	}

	query += ` WHERE c.jid = ?`

	var chat Chat
	var lastMessageTimeStr sql.NullString
	var lastMessage sql.NullString
	var lastSender sql.NullString
	var lastIsFromMe sql.NullBool
	var name sql.NullString

	err := wa.db.QueryRow(query, chatJID).Scan(
		&chat.JID,
		&name,
		&lastMessageTimeStr,
		&lastMessage,
		&lastSender,
		&lastIsFromMe,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("database error: %v", err)
	}

	if name.Valid {
		chat.Name = name.String
	}

	if lastMessageTimeStr.Valid {
		chat.LastMessageTime, _ = time.Parse("2006-01-02 15:04:05", lastMessageTimeStr.String)
	}

	if lastMessage.Valid {
		chat.LastMessage = lastMessage.String
	}

	if lastSender.Valid {
		chat.LastSender = lastSender.String
	}

	if lastIsFromMe.Valid {
		chat.LastIsFromMe = lastIsFromMe.Bool != false
	}

	return &chat, nil
}

// GetDirectChatByContact gets chat metadata by sender phone number
func (wa *WhatsApp) GetDirectChatByContact(senderPhoneNumber string) (*Chat, error) {
	var chat Chat
	var lastMessageTimeStr sql.NullString
	var lastMessage sql.NullString
	var lastSender sql.NullString
	var lastIsFromMe sql.NullBool
	var name sql.NullString

	err := wa.db.QueryRow(`
		SELECT 
			c.jid,
			c.name,
			c.last_message_time,
			m.content as last_message,
			m.sender as last_sender,
			m.is_from_me as last_is_from_me
		FROM chats c
		LEFT JOIN messages m ON c.jid = m.chat_jid 
			AND c.last_message_time = m.timestamp
		WHERE c.jid LIKE ? AND c.jid NOT LIKE '%@g.us'
		LIMIT 1
	`, "%"+senderPhoneNumber+"%").Scan(
		&chat.JID,
		&name,
		&lastMessageTimeStr,
		&lastMessage,
		&lastSender,
		&lastIsFromMe,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("database error: %v", err)
	}

	if name.Valid {
		chat.Name = name.String
	}

	if lastMessageTimeStr.Valid {
		chat.LastMessageTime, _ = time.Parse("2006-01-02 15:04:05", lastMessageTimeStr.String)
	}

	if lastMessage.Valid {
		chat.LastMessage = lastMessage.String
	}

	if lastSender.Valid {
		chat.LastSender = lastSender.String
	}

	if lastIsFromMe.Valid {
		chat.LastIsFromMe = lastIsFromMe.Bool != false
	}

	return &chat, nil
}