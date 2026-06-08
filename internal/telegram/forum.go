package telegram

// Форум-темы супергрупп: список тем, история темы (как тред ответов) и отправка
// в тему (reply_to на корень темы).

import (
	"context"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

// Topic — тема форум-супергруппы.
type Topic struct {
	ID     int
	Title  string
	Closed bool
}

// ForumTopics возвращает темы форум-супергруппы.
func (s *Session) ForumTopics(ctx context.Context, peer tg.InputPeerClass, limit int) ([]Topic, error) {
	if limit <= 0 {
		limit = 100
	}
	res, err := s.api.MessagesGetForumTopics(ctx, &tg.MessagesGetForumTopicsRequest{Peer: peer, Limit: limit})
	if err != nil {
		return nil, err
	}
	var out []Topic
	for _, tc := range res.Topics {
		if t, ok := tc.(*tg.ForumTopic); ok {
			out = append(out, Topic{ID: t.ID, Title: sanitize(t.Title), Closed: t.Closed})
		}
	}
	return out, nil
}

// HistoryByTopic загружает сообщения темы (тред ответов на корень темы),
// развёрнутые старые-сверху.
func (s *Session) HistoryByTopic(ctx context.Context, peer tg.InputPeerClass, topicID, limit int) ([]HistoryMessage, error) {
	return s.HistoryByTopicBeforeID(ctx, peer, topicID, 0, limit)
}

// HistoryByTopicBeforeID — как HistoryByTopic, но возвращает сообщения темы
// старше beforeID (для докрутки истории темы в прошлое). beforeID==0 — с самых
// свежих.
func (s *Session) HistoryByTopicBeforeID(ctx context.Context, peer tg.InputPeerClass, topicID int, beforeID int64, limit int) ([]HistoryMessage, error) {
	if limit <= 0 {
		limit = 20
	}
	b := messages.NewQueryBuilder(s.api).GetReplies(peer).MsgID(topicID).BatchSize(limit)
	if beforeID > 0 {
		b = b.OffsetID(int(beforeID))
	}
	return collectHistory(ctx, b.Iter(), limit)
}

// SendToTopic отправляет сообщение в тему форума (reply_to на корень темы).
func (s *Session) SendToTopic(ctx context.Context, peer tg.InputPeerClass, topicID int, text string) (SentMessage, error) {
	upd, err := message.NewSender(s.api).To(peer).Reply(topicID).Text(ctx, text)
	if err != nil {
		return SentMessage{}, err
	}
	return sentFromUpdates(upd), nil
}
