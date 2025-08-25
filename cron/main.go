package cron

type JobConfig struct {
	Name  string
	Spec  string
	Class Job
}

var (
	jobs      = make([]JobConfig, 0)
	cronClass *Cron
)

func Register(config JobConfig) {
	jobs = append(jobs, config)
}

func Init() {
	cronClass = New(WithSeconds())

	for _, config := range jobs {
		_, _ = cronClass.AddJob(config.Spec, config.Class)
	}

	cronClass.Start()
}
