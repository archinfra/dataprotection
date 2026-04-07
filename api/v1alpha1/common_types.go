package v1alpha1

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BackupDriver 定义支持的数据库/中间件类型，即数据驱动类型
type BackupDriver string

const (
	BackupDriverMySQL    BackupDriver = "mysql"
	BackupDriverRedis    BackupDriver = "redis"
	BackupDriverMongoDB  BackupDriver = "mongodb"
	BackupDriverMinIO    BackupDriver = "minio"
	BackupDriverRabbitMQ BackupDriver = "rabbitmq"
	BackupDriverMilvus   BackupDriver = "milvus"
)

// RepositoryType 定义备份存储库的类型
type RepositoryType string

const (
	RepositoryTypeNFS RepositoryType = "nfs"
	RepositoryTypeS3  RepositoryType = "s3"
)

// ResourcePhase 定义资源当前的生命周期阶段
type ResourcePhase string

const (
	// ResourcePhasePending 资源已创建但尚未开始处理
	ResourcePhasePending ResourcePhase = "Pending"
	// ResourcePhaseReady 资源已就绪，可以开始备份/恢复
	ResourcePhaseReady ResourcePhase = "Ready"
	// ResourcePhaseRunning 备份/恢复任务正在执行中
	ResourcePhaseRunning ResourcePhase = "Running"
	// ResourcePhaseSucceeded 任务成功完成
	ResourcePhaseSucceeded ResourcePhase = "Succeeded"
	// ResourcePhaseFailed 任务失败
	ResourcePhaseFailed ResourcePhase = "Failed"
	// ResourcePhasePaused 定时备份策略被暂停
	ResourcePhasePaused ResourcePhase = "Paused"
)

// VerificationMode 定义备份验证的模式
type VerificationMode string

const (
	// VerificationModeNone 不进行验证
	VerificationModeNone VerificationMode = "None"
	// VerificationModeMetadata 仅验证备份元数据（如文件完整性）
	VerificationModeMetadata VerificationMode = "Metadata"
	// VerificationModeRestoreJob 通过执行恢复 Job 来实际验证备份可恢复性
	VerificationModeRestoreJob VerificationMode = "RestoreJob"
)

// RestoreTargetMode 定义恢复时的目标模式
type RestoreTargetMode string

const (
	// RestoreTargetModeInPlace 原地恢复：直接恢复到原实例或原位置
	RestoreTargetModeInPlace RestoreTargetMode = "InPlace"
	// RestoreTargetModeOutOfPlace 异机恢复：恢复到新的实例或新位置
	RestoreTargetModeOutOfPlace RestoreTargetMode = "OutOfPlace"
)

// SecretKeyReference 用于从 Kubernetes Secret 中引用一个键值
type SecretKeyReference struct {
	// Name Secret 的名称（位于同一命名空间）
	Name string `json:"name"`
	// Key Secret 中存放敏感数据的字段名
	Key string `json:"key"`
}

// ServiceReference 引用一个 Kubernetes Service，用于获取连接端点
type ServiceReference struct {
	// Name Service 名称
	Name string `json:"name"`
	// Namespace Service 所在命名空间（默认为当前资源的命名空间）
	Namespace string `json:"namespace,omitempty"`
	// Port Service 的端口号
	Port int32 `json:"port,omitempty"`
}

// NamespacedObjectReference 引用任意 Kubernetes 对象
type NamespacedObjectReference struct {
	// APIVersion 对象的 API 版本
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind 对象的资源类型
	Kind string `json:"kind,omitempty"`
	// Namespace 对象所在命名空间
	Namespace string `json:"namespace,omitempty"`
	// Name 对象名称
	Name string `json:"name"`
}

// EndpointSpec 描述一个数据服务的连接端点
type EndpointSpec struct {
	// Host 服务主机名或 IP
	Host string `json:"host,omitempty"`
	// Port 服务端口
	Port int32 `json:"port,omitempty"`
	// Scheme 连接协议（如 http, https, tcp）
	Scheme string `json:"scheme,omitempty"`
	// ServiceRef 可选的 Service 引用，优先级高于 Host/Port
	ServiceRef *ServiceReference `json:"serviceRef,omitempty"`
	// Username 明文用户名（不推荐）
	Username string `json:"username,omitempty"`
	// UsernameFrom 从 Secret 中获取用户名
	UsernameFrom *SecretKeyReference `json:"usernameFrom,omitempty"`
	// PasswordFrom 从 Secret 中获取密码（必填）
	PasswordFrom *SecretKeyReference `json:"passwordFrom,omitempty"`
}

// BackupScheduleSpec 定义备份的定时调度策略（基于 Cron）
type BackupScheduleSpec struct {
	// Cron 标准 Cron 表达式，例如 "0 2 * * *"（每天凌晨2点）
	Cron string `json:"cron,omitempty"`
	// Suspend 是否暂停调度
	Suspend bool `json:"suspend,omitempty"`
	// StartingDeadlineSeconds 调度启动的截止时间（秒），超时则跳过本次调度
	StartingDeadlineSeconds *int64 `json:"startingDeadlineSeconds,omitempty"`
	// ConcurrencyPolicy 并发策略：Allow（允许并发）、Forbid（禁止并发）、Replace（替换旧任务）
	ConcurrencyPolicy batchv1.ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`
}

// RetentionPolicy 定义备份的保留策略
type RetentionPolicy struct {
	// KeepLast 保留最近多少个备份，旧备份将被自动清理
	KeepLast int32 `json:"keepLast,omitempty"`
}

// VerificationSpec 定义备份完成后的验证配置
type VerificationSpec struct {
	// Enabled 是否启用验证
	Enabled bool `json:"enabled,omitempty"`
	// Mode 验证模式
	Mode VerificationMode `json:"mode,omitempty"`
}

// ExecutionTemplateSpec 定义备份/恢复 Job 的执行模板，对应 Kubernetes Pod 模板的子集
type ExecutionTemplateSpec struct {
	// RunnerImage 执行备份/恢复操作的主容器镜像
	RunnerImage string `json:"runnerImage,omitempty"`
	// HelperImage 辅助容器镜像（如 mc、mysqldump 客户端等）
	HelperImage string `json:"helperImage,omitempty"`
	// ServiceAccountName 运行 Job 时使用的 ServiceAccount
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// ImagePullPolicy 镜像拉取策略（Always、Never、IfNotPresent）
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	// Command 覆盖容器默认入口命令
	Command []string `json:"command,omitempty"`
	// Args 传递给命令的参数
	Args []string `json:"args,omitempty"`
	// BackoffLimit Job 失败后的重试次数限制
	BackoffLimit *int32 `json:"backoffLimit,omitempty"`
	// TTLSecondsAfterFinished Job 完成后保留的秒数，过期后自动清理
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
	// NodeSelector 将 Job Pod 调度到匹配标签的节点
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Tolerations 允许 Pod 调度到带有污点的节点
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Resources 容器的资源请求和限制（CPU/内存）
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// ExtraEnv 注入到主容器的额外环境变量
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`
}

// DriverConfig 各数据驱动的专用配置（联合类型）
type DriverConfig struct {
	// MySQL MySQL 驱动配置
	MySQL *MySQLDriverConfig `json:"mysql,omitempty"`
	// Redis Redis 驱动配置
	Redis *RedisDriverConfig `json:"redis,omitempty"`
	// MongoDB MongoDB 驱动配置
	MongoDB *MongoDBDriverConfig `json:"mongodb,omitempty"`
	// MinIO MinIO 驱动配置
	MinIO *MinIODriverConfig `json:"minio,omitempty"`
	// RabbitMQ RabbitMQ 驱动配置
	RabbitMQ *RabbitMQDriverConfig `json:"rabbitmq,omitempty"`
	// Milvus Milvus 驱动配置
	Milvus *MilvusDriverConfig `json:"milvus,omitempty"`
}

// MySQLDriverConfig MySQL 备份/恢复的特定配置
type MySQLDriverConfig struct {
	// Mode 操作模式：逻辑备份（logical）或物理备份（physical）
	Mode string `json:"mode,omitempty"`
	// Databases 要备份的数据库列表（为空表示全部）
	Databases []string `json:"databases,omitempty"`
	// Tables 要备份的表列表，格式为 "database.table"，为空表示数据库内全部表
	Tables []string `json:"tables,omitempty"`
	// RestoreMode 恢复模式：merge（合并）、wipe-all-user-databases（清空所有用户数据库后恢复）
	RestoreMode string `json:"restoreMode,omitempty"`
}

// RedisDriverConfig Redis 备份/恢复的特定配置
type RedisDriverConfig struct {
	// Mode 模式：rdb（快照）、aof（追加日志）或混合
	Mode string `json:"mode,omitempty"`
	// Databases 要备份的数据库索引列表（-1 表示所有）
	Databases []int32 `json:"databases,omitempty"`
	// KeyPrefix 仅备份匹配指定前缀的键（支持通配符）
	KeyPrefix []string `json:"keyPrefix,omitempty"`
}

// MongoDBDriverConfig MongoDB 备份/恢复的特定配置
type MongoDBDriverConfig struct {
	// Databases 要备份的数据库列表（为空表示全部）
	Databases []string `json:"databases,omitempty"`
	// Collections 要备份的集合列表，格式 "database.collection"
	Collections []string `json:"collections,omitempty"`
	// IncludeUsersRoles 是否同时备份用户和角色定义
	IncludeUsersRoles bool `json:"includeUsersRoles,omitempty"`
}

// MinIODriverConfig MinIO 备份/恢复的特定配置
type MinIODriverConfig struct {
	// Buckets 要备份的桶列表（为空表示全部）
	Buckets []string `json:"buckets,omitempty"`
	// Prefixes 仅备份桶内指定前缀的对象
	Prefixes []string `json:"prefixes,omitempty"`
	// IncludeVersions 是否备份对象的历史版本（需启用版本控制）
	IncludeVersions bool `json:"includeVersions,omitempty"`
}

// RabbitMQDriverConfig RabbitMQ 备份/恢复的特定配置
type RabbitMQDriverConfig struct {
	// IncludeDefinitions 是否备份交换机、队列、绑定等定义
	IncludeDefinitions bool `json:"includeDefinitions,omitempty"`
	// Vhosts 要备份的虚拟主机列表（为空表示所有 vhost）
	Vhosts []string `json:"vhosts,omitempty"`
	// Queues 要备份的队列列表，格式 "vhost/queueName"
	Queues []string `json:"queues,omitempty"`
}

// MilvusDriverConfig Milvus 备份/恢复的特定配置
type MilvusDriverConfig struct {
	// Databases 要备份的数据库列表（Milvus 2.2+ 支持多库）
	Databases []string `json:"databases,omitempty"`
	// Collections 要备份的集合列表，格式 "database.collection"
	Collections []string `json:"collections,omitempty"`
	// IncludeObjectStorage 是否同时备份底层的对象存储数据
	IncludeObjectStorage bool `json:"includeObjectStorage,omitempty"`
}

// RepositoryEndpointSpec 定义备份存储库的路径（适用于 NFS 或 S3 前缀）
type RepositoryEndpointSpec struct {
	// Path 存储库中的基本路径（目录或前缀）
	Path string `json:"path,omitempty"`
}

// NFSRepositorySpec NFS 类型的存储库配置
type NFSRepositorySpec struct {
	// Server NFS 服务器地址（IP 或主机名）
	Server string `json:"server"`
	// Path NFS 导出的共享路径
	Path string `json:"path"`
}

// S3RepositorySpec S3 兼容对象存储的配置
type S3RepositorySpec struct {
	// Endpoint S3 服务端点（如 https://s3.amazonaws.com 或 http://minio:9000）
	Endpoint string `json:"endpoint"`
	// Bucket 存储桶名称
	Bucket string `json:"bucket"`
	// Prefix 对象键前缀（相当于子目录）
	Prefix string `json:"prefix,omitempty"`
	// Region 存储桶所在区域（可选，部分兼容服务需要）
	Region string `json:"region,omitempty"`
	// Insecure 是否跳过 TLS 证书验证（仅测试环境）
	Insecure bool `json:"insecure,omitempty"`
	// AccessKeyFrom 从 Secret 中获取 AccessKey
	AccessKeyFrom *SecretKeyReference `json:"accessKeyFrom,omitempty"`
	// SecretKeyFrom 从 Secret 中获取 SecretKey
	SecretKeyFrom *SecretKeyReference `json:"secretKeyFrom,omitempty"`
	// SessionTokenFrom 从 Secret 中获取临时会话 Token（可选，用于 STS）
	SessionTokenRef *SecretKeyReference `json:"sessionTokenFrom,omitempty"`
}

// RestoreTargetSpec 定义恢复操作的目标实例或位置
type RestoreTargetSpec struct {
	// Mode 恢复模式：原地恢复或异机恢复
	Mode RestoreTargetMode `json:"mode,omitempty"`
	// TargetRef 引用一个已存在的 Kubernetes 对象（如 BackupSource 或 Service）
	TargetRef *NamespacedObjectReference `json:"targetRef,omitempty"`
	// Endpoint 显式指定的连接端点（优先级高于 TargetRef）
	Endpoint *EndpointSpec `json:"endpoint,omitempty"`
	// DriverConfig 驱动级别的恢复配置（例如 MySQL 的恢复模式）
	DriverConfig DriverConfig `json:"driverConfig,omitempty"`
}

// RepositoryRunStatus 记录一次备份或恢复执行的运行时状态
type RepositoryRunStatus struct {
	// Name 关联的 Job 名称或执行 ID
	Name string `json:"name,omitempty"`
	// Phase 当前执行阶段（Pending/Running/Succeeded/Failed）
	Phase ResourcePhase `json:"phase,omitempty"`
	// Message 人类可读的状态详情或错误信息
	Message string `json:"message,omitempty"`
	// Snapshot 备份产出物的标识（如 S3 路径或快照 ID）
	Snapshot string `json:"snapshot,omitempty"`
	// UpdatedAt 状态最近更新时间
	UpdatedAt *metav1.Time `json:"updatedAt,omitempty"`
}
