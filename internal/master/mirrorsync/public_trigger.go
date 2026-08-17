package mirrorsync

import (
	"context"
	"errors"

	"mirror-server/internal/config"
)

func (s Service) TriggerFullPublicNotification(ctx context.Context) (int, error) {
	notifier, ok := s.Scanner.Notifier.(ImmediatePublicChangeNotifier)
	if !ok {
		return 0, errors.New("IndexNow 通知未启用")
	}
	projects := config.Projects{}
	if s.Projects != nil {
		loaded, err := s.Projects.Load()
		if err != nil {
			projects = s.Projects.Current()
		} else {
			projects = loaded
		}
	}
	snapshot, err := s.Scanner.PublicSnapshot(ctx, projects)
	if err != nil {
		return 0, errors.New("公开项目列表读取失败")
	}
	return notifier.NotifySnapshotNow(ctx, snapshot), nil
}
