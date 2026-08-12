import { expect, type Locator, type Page, test } from '@playwright/test'
import {
  collectConsoleDiagnostics,
  expectNoAxeViolations,
  expectNoConsoleDiagnostics,
  expectNoDocumentOverflow,
  expectOpaqueBackground,
  gotoConsoleRoute,
} from './helpers'
import { installConsoleApiMocks } from './route-mocks'

const zh = {
  apiKeys: '\u0041\u0050\u0049\u0020\u006b\u0065\u0079',
  apiKeyRevoked: '\u5df2\u64a4\u9500',
  auditLog: '\u5ba1\u8ba1\u65e5\u5fd7',
  actionInProgress: '\u5904\u7406\u4e2d',
  actionOpen: '\u5f85\u5904\u7406',
  actionVerified: '\u5df2\u9a8c\u8bc1',
  closedLoopProof:
    '\u0031\u0020\u4e2a\u5f00\u653e\u4f4e\u5206\u0020\u00b7\u0020\u0031\u0020\u4e2a\u903e\u671f\u0020\u00b7\u0020\u54cd\u5e94\u7387\u0020\u0035\u0030\u0025',
  closedLoopRecovery: '\u95ed\u73af\u6062\u590d',
  closedLoopRecoveryRisk: '\u95ed\u73af\u6062\u590d\u5065\u5eb7\u5ea6\u504f\u4f4e',
  classificationQuality: '分类质量',
  anomalies: '异常检测',
  classificationReviewLearning: 'AI 审核学习',
  classificationTrainingCandidates: '11 个训练候选',
  controlTower: '\u63a7\u5236\u5854',
  customerRequests: '\u5ba2\u6237\u9700\u6c42',
  customerRequestsEmpty: '\u8fd8\u6ca1\u6709\u5ba2\u6237\u9700\u6c42',
  customerRequestsAccountFilter: '\u8d26\u6237\u6807\u8bc6',
  customerRequestsAccountView:
    '\u67e5\u770b\u0020\u0041\u0063\u006d\u0065\u0020\u0043\u006f\u0072\u0070\u0020\u7684\u5ba2\u6237\u9700\u6c42',
  customerRequestsAccountOverview: '\u8d26\u6237\u4fe1\u53f7\u6982\u89c8',
  customerRequestsAccountOverviewScope:
    '\u0041\u0063\u006d\u0065\u0020\u0043\u006f\u0072\u0070\u0020\u9700\u6c42\u7ec4\u5408',
  customerRequestsAccountOverviewProfile:
    '\u0065\u006e\u0074\u0065\u0072\u0070\u0072\u0069\u0073\u0065\u0020\u00b7\u0020\u006d\u0069\u0064\u005f\u006d\u0061\u0072\u006b\u0065\u0074\u0020\u00b7\u0020\u0061\u0063\u0074\u0069\u0076\u0065\u0020\u00b7\u0020\u0073\u0061\u006c\u0065\u0073\u0066\u006f\u0072\u0063\u0065',
  customerRequestsAccountOverviewRequests: '\u0031\u0020\u4e2a\u9700\u6c42',
  customerRequestsAccountOverviewRevenue:
    '\u6536\u5165\u5f71\u54cd\u0020\u0024\u0032\u0034\u002c\u0030\u0030\u0030',
  customerRequestsAccountOverviewAverageScore:
    '\u5e73\u5747\u51b3\u7b56\u5206\u0020\u0031\u0031\u0034',
  customerRequestsAccountOverviewTopScore: '\u6700\u9ad8\u51b3\u7b56\u5206\u0020\u0031\u0031\u0034',
  customerRequestsAccountOverviewSignals: '\u51b3\u7b56\u4fe1\u53f7',
  customerRequestsAccountOverviewSignalHighPriority:
    '\u0031\u0020\u4e2a\u9ad8\u4f18\u5148\u7ea7\u9700\u6c42\uff0c\u6700\u9ad8\u5206\u0020\u0031\u0031\u0034',
  customerRequestsAccountOverviewSignalRevenue:
    '\u6536\u5165\u5f71\u54cd\u0020\u0024\u0032\u0034\u002c\u0030\u0030\u0030\uff0c\u5e73\u5747\u5206\u0020\u0031\u0031\u0034',
  customerRequestsAccountOverviewSignalEvidence:
    '\u0034\u0020\u6761\u5ba2\u6237\u8bc1\u636e\uff0c\u5e73\u5747\u5206\u0020\u0031\u0031\u0034',
  customerRequestsDecisionBreakdown: '\u51b3\u7b56\u5206\u89e3\u91ca',
  customerRequestsDecisionBreakdownTotal: '\u8ba1\u5206\u8d21\u732e\u0020\u0031\u0031\u0034',
  customerRequestsDecisionFactorRevenue:
    '\u0024\u0033\u0033\u002c\u0030\u0030\u0030\u0020\u00b7\u0020\u6bcf\u0020\u0024\u0031\u002c\u0030\u0030\u0030\u0020\u8ba1\u0020\u0031\u0020\u5206\u0020\u00b7\u0020\u4e0a\u9650\u0020\u0031\u0030\u0030',
  customerRequestsEvidenceQuality:
    '\u8bc1\u636e\u8d28\u91cf\u0020\u0037\u0035\u0020\u00b7\u0020\u9ad8',
  customerRequestsEvidenceQualityStrength: '\u591a\u6765\u6e90\u8bc1\u636e',
  customerRequestsDecisionRecords: '\u51b3\u7b56\u8bb0\u5f55',
  customerRequestsDecisionRecordStatus:
    '\u72b6\u6001\u0020\u004f\u0070\u0065\u006e\u0020\u2192\u0020\u0050\u006c\u0061\u006e\u006e\u0065\u0064',
  customerRequestsDecisionOwner: '\u8d1f\u8d23\u4eba\u0020owner@example.com',
  customerRequestsDecisionRationale:
    '\u539f\u56e0\u0020priority=high feedback=2 customers=1 accounts=1 votes=1 revenue_cents=3300000 delivery_health=synced',
  customerRequestsDecisionEvidenceBundle:
    '\u8bc1\u636e\u5305\u0020customer-request/11111111-1111-1111-1111-111111111111/evidence/CR-1',
  customerRequestsDecisionPublicSafe: '\u516c\u5f00\u5b89\u5168\u0020\u9700\u590d\u6838',
  customerRequestsDecisionPublicReason: '\u6536\u5165\u4e0a\u4e0b\u6587',
  customerRequestsAccountOverviewEvents: '\u8d26\u6237\u8bc1\u636e\u65f6\u95f4\u7ebf',
  customerRequestsAccountEventFeedbackLinked: '\u53cd\u9988\u5173\u8054',
  customerRequestsAccountEventIssueSynced: '\u4ea4\u4ed8\u540c\u6b65',
  customerRequestsAccountEventNoteAdded: '\u5185\u90e8\u5907\u6ce8',
  customerRequestsAccountOverviewTimeline: '\u8bf7\u6c42\u65f6\u95f4\u7ebf',
  recoveryCommand: '\u95ed\u73af\u6062\u590d\u6307\u6325',
  recoveryCommandNext:
    '\u5148\u5904\u7406\u0020\u0031\u0020\u4e2a\u903e\u671f\u6062\u590d\u5e76\u66f4\u65b0\u72b6\u6001\u3002',
  recoveryOwner:
    '\u006d\u0065\u006d\u0062\u0065\u0072\u002d\u0061\u0031\u0031\u0079\u002d\u0063\u0073',
  firstValueActivation: '\u9996\u6b21\u4ef7\u503c\u6fc0\u6d3b',
  firstValueProgress: '\u0032\u0020\u002f\u0020\u0035\u0020\u5df2\u8fbe\u6807',
  readinessMatrix: '\u4e16\u754c\u7ea7\u53ef\u7528\u6027\u77e9\u9635',
  releaseEvidence: '\u53d1\u5e03\u9a8c\u8bc1\u8bc1\u636e',
  releaseEvidenceContract: '\u0036\u0020\u002f\u0020\u0036\u0020\u9879\u5df2\u7eb3\u5165',
  releaseEvidenceProductContract: '\u4ea7\u54c1\u5408\u540c\u95e8\u7981',
  releaseVerification: '\u53d1\u5e03\u9a8c\u8bc1\u53ef\u8ffd\u6eaf',
  reliability: '\u53ef\u9760\u6027\u603b\u89c8',
  reliabilityReplayDrill:
    '\u0052\u0065\u0070\u006c\u0061\u0079\u0020\u002f\u0020\u0042\u0061\u0063\u006b\u0066\u0069\u006c\u006c\u0020\u6f14\u7ec3',
  reliabilityErrorBudgetLedger:
    '\u0045\u0072\u0072\u006f\u0072\u0020\u0062\u0075\u0064\u0067\u0065\u0074\u0020\u002f\u0020\u0062\u0075\u0072\u006e\u002d\u0072\u0061\u0074\u0065\u0020\u006c\u0065\u0064\u0067\u0065\u0072',
  reliabilityReleaseHealthLedger:
    '\u0052\u0065\u006c\u0065\u0061\u0073\u0065\u0020\u0068\u0065\u0061\u006c\u0074\u0068\u0020\u0063\u006f\u0072\u0072\u0065\u006c\u0061\u0074\u0069\u006f\u006e\u0020\u006c\u0065\u0064\u0067\u0065\u0072',
  reliabilityIncidentTimeline:
    '\u0049\u006e\u0063\u0069\u0064\u0065\u006e\u0074\u0020\u0074\u0069\u006d\u0065\u006c\u0069\u006e\u0065\u0020\u0072\u0065\u0063\u006f\u006e\u0073\u0074\u0072\u0075\u0063\u0074\u0069\u006f\u006e',
  reliabilityTenantQuota:
    '\u0054\u0065\u006e\u0061\u006e\u0074\u0020\u0071\u0075\u006f\u0074\u0061\u0020\u002f\u0020\u0073\u0061\u0074\u0075\u0072\u0061\u0074\u0069\u006f\u006e\u0020\u0064\u0061\u0073\u0068\u0062\u006f\u0061\u0072\u0064',
  reliabilityBackupRestore: 'Backup / restore drill evidence',
  reliabilityConsistency: 'Data consistency checks',
  reliabilityPipelineSlo: 'Pipeline SLO ledger',
  reliabilityReplayOutbox:
    '\u004f\u0075\u0074\u0062\u006f\u0078\u0020\u0062\u0075\u0072\u006e\u0020\u0078',
  signalCaptured: '\u5ba2\u6237\u4fe1\u53f7\u5df2\u8fdb\u5165',
  signalQualityTrust: '\u4fe1\u53f7\u7406\u89e3\u53ef\u4fe1',
  sourceConnected: '\u63a5\u5165\u6e90\u5df2\u8fde\u63a5',
  sourceHealth: '\u4fe1\u53f7\u5165\u53e3\u5065\u5eb7',
  sourceHealthError: '\u6e90\u7ea7\u9519\u8bef',
  sourceHealthNext:
    '\u5148\u4fee\u590d\u0020\u0031\u0020\u4e2a\u6e90\u7ea7\u9519\u8bef\uff0c\u907f\u514d\u4fe1\u53f7\u5165\u53e3\u4e22\u5931\u3002',
  sourceHealthSource:
    '\u0053\u006c\u0061\u0063\u006b\u0020\u004d\u006f\u0063\u006b\u0020\u0053\u006f\u0075\u0072\u0063\u0065',
  worldClassMaturity: '\u0031\u0030\u0030\u9879\u4e16\u754c\u7ea7\u6210\u719f\u5ea6\u5dee\u8ddd',
  worldClassMaturityGap: '\u0033\u0031\u0020\u9879\u4ecd\u963b\u585e\u4e16\u754c\u7ea7\u4f53\u9a8c',
  worldClassMaturityIdentity: '\u8eab\u4efd\u4e0e\u8d26\u6237\u4e0a\u4e0b\u6587',
  worldClassMaturityAccount:
    '\u0041\u0063\u0063\u006f\u0075\u006e\u0074\u002f\u0043\u006f\u006d\u0070\u0061\u006e\u0079\u0020\u4e00\u7b49\u516c\u6c11\u6a21\u578b',
  worldClassExecution: '\u4e16\u754c\u7ea7\u6267\u884c\u961f\u5217',
  worldClassExecutionCount: '\u0031\u0020\u4e2a\u4f18\u5148\u5207\u7247',
  worldClassExecutionReleaseHealth:
    '\u53d1\u5e03\u7248\u672c\u3001\u751f\u547d\u5468\u671f\u3001\u6062\u590d\u7ed3\u679c\u3001\u53cd\u9988\u538b\u529b\u548c\u901a\u77e5\u5931\u8d25\u8fdb\u5165\u540c\u4e00\u4e2a\u0020\u0072\u0065\u006c\u0065\u0061\u0073\u0065\u0020\u0068\u0065\u0061\u006c\u0074\u0068\u0020\u006c\u0065\u0064\u0067\u0065\u0072\u3002',
  worldClassExecutionNorthStarMetrics:
    'First value time、signal loss rate、decision coverage、closed-loop rate、connector freshness 和 developer activation 能进入同一个北极星指标面板。',
  developerAdoptionKit: '开发者 API 采用包',
  developerAdoptionFingerprint:
    '2 scopes / 2 presets / 1 active keys / 1 service accounts / 14/14 artifacts verified',
  developerAdoptionSummary: 'developer API adoption kit evidence is ready',
  developerAdoptionOpenApi: '2 scopes / 2 presets / 1 active keys / 1 used',
  developerAdoptionNode: '1 Node examples / e2e on / browser smoke on / 1 automation identities',
  developerAdoptionGo: '1 Go examples / e2e on / 1 active keys',
  developerAdoptionSandbox: '1 active keys / 1 service accounts / 2 presets / demo bootstrap on',
  developerAdoptionReplay: '4 replay fixtures / replay smoke on / browser ingest on',
  developerSdkParityGate: '开发者 SDK parity gate',
  developerSdkParityFingerprint:
    '35/35 shared methods / verifier on / 0 browser-safe keys / 6/6 release gates',
  developerSdkParitySummary: '1 SDK parity lanes need hardening',
  developerSdkParityManagement: '35 shared methods / Node 35 / Go 35 / drift 0',
  developerSdkParityError:
    'ErrorResponse + ErrorCode / AttuneError + TransportErrorCode / envelope available',
  developerSdkParityRetry:
    '408/429/5xx / Retry-After / idempotency available / API version pinned on',
  developerSdkParityBrowser: '0 browser-safe keys / 1 management scoped keys / browser smoke on',
  developerSdkParityRelease: 'npm ESM+CJS+types / Go module / live e2e 2/2 / packed smoke on',
  developerApiConsistencyContract: '开发者 API consistency contract',
  developerApiConsistencyFingerprint:
    '3/3 public pagination surfaces / 3/3 console mirrors / 3/3 filters / 3/3 sort enums / verifier on',
  developerApiConsistencySummary: 'developer API consistency contract is verified',
  developerApiConsistencyPagination:
    '2 cursor surfaces / 1 before_id surface / nextCursor + nextBeforeId',
  developerApiConsistencyFilter: 'audit actions + actor/target/time / request_type / status[]',
  developerApiConsistencySort:
    'CustomerRequestSort + SortDirection / decision score / delivery health',
  developerApiConsistencyError: 'ErrorResponse code/message/requestId across OpenAPI, Node, and Go',
  developerApiConsistencyIdempotency: 'Idempotency-Key / management POST coverage 2/2',
  developerApiConsistencyWire:
    'actions[]=repeat / request_type / before_id / positive limit validators',
  developerImportExportWorkbench: '开发者 import/export 工作台',
  developerImportExportFingerprint:
    '2/2 formats / 4 templates / 4/4 required mappings / dry-run 37 create 2 update 1 reject / 4 recovery classes / verifier on',
  developerImportExportSummary: 'developer import/export workbench is verified',
  developerImportExportTemplate: 'CSV + JSON / 2 import templates / 2 export templates',
  developerImportExportSchema: '8 fields / 4 required / 3 samples',
  developerImportExportMapping: '8 mapped fields / 4 required tracked',
  developerImportExportDryRun: '37 create / 2 update / 1 reject',
  developerImportExportRecovery: 'quarantine / map_status / merge_or_skip / request_scope',
  developerImportExportGovernance:
    'feedback:write / customer_request:write / audit:read / import + export events',
  blockedStatus: '\u963b\u585e',
  passStatus: '\u8fbe\u6807',
  currentPassword: '\u5f53\u524d\u5bc6\u7801',
  effectiveScopes: '\u751f\u6548\u6743\u9650',
  feedback: '\u53cd\u9988',
  feedbackAccountFilter: '\u8d26\u6237\u6807\u8bc6',
  feedbackAccountChip: '\u8d26\u6237\u0020\u0041\u0063\u006d\u0065\u0020\u0043\u006f\u0072\u0070',
  feedbackAccountContext: '\u8d26\u6237\u4e0a\u4e0b\u6587',
  feedbackTriageCommand: '\u53cd\u9988\u8fd0\u8425\u6307\u6325\u4e2d\u5fc3',
  identityReviewTitle: '\u8eab\u4efd\u5408\u5e76\u590d\u6838\u961f\u5217',
  identityReviewCandidate: '\u6700\u9ad8\u4f18\u5148\u7ea7\u5019\u9009',
  identityReviewNeedsEvidence: '\u8bc1\u636e\u4e0d\u8db3\u6837\u672c',
  identityReviewApproveMerge: '\u6279\u51c6\u5408\u5e76',
  identityReviewMergeSuccess: '\u5df2\u5408\u5e76\u5230 ada@example.com',
  identityReviewUndoMerge: '\u64a4\u9500\u5408\u5e76',
  identityReviewSplitSuccess: '\u5df2\u64a4\u9500 ada@example.com',
  identityReviewSubjects: '\u5df2\u89e3\u6790\u4e3b\u4f53',
  identityReviewSubjectDetail: '\u4e3b\u4f53\u8be6\u60c5',
  identityReviewTimeline: '\u65f6\u95f4\u7ebf',
  identityReviewRevoked: '\u5df2\u64a4\u9500',
  identityEvidence: '\u8eab\u4efd\u56fe\u8bc1\u636e',
  identityCandidateCount: '\u0034\u0020\u4e2a\u5408\u5e76\u5019\u9009',
  identityResolutionTitle: '\u89e3\u6790\u8d28\u91cf',
  identityResolutionAction: '\u8fdb\u5165\u5408\u5e76\u590d\u6838',
  identityResolutionCounts:
    '\u0033\u0020\u4e2a\u7a33\u5b9a\u952e\u0020\u00b7\u0020\u0034\u0020\u4e2a\u6765\u6e90\u8def\u5f84',
  signalTrace: '端到端证据链',
  feedbackRetryFailed: '\u91cd\u8bd5\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u518d\u8bd5',
  feedbackRetryQueued: '\u5df2\u52a0\u5165\u91cd\u8bd5\u961f\u5217',
  assignmentTitle: '\u8d23\u4efb\u4e0e\u0020\u0053\u004c\u0041',
  assignmentOwner: '\u8d1f\u8d23\u4eba',
  assignmentDue: '\u0053\u004c\u0041\u0020\u622a\u6b62',
  assignmentNote: '\u5904\u7406\u5907\u6ce8',
  assignmentSave: '\u4fdd\u5b58\u5206\u6d3e',
  assignmentSaved: '\u5206\u6d3e\u5df2\u4fdd\u5b58',
  assignmentEscalationTitle: '\u9700\u8981\u7acb\u5373\u5904\u7406\u7684\u8d23\u4efb\u7f3a\u53e3',
  assignmentEscalationOpenFeedback101:
    '\u6253\u5f00\u53cd\u9988\u0020\u0023\u0066\u0065\u0065\u0064\u0062\u0061\u0063\u006b\u002d\u0031\u0030\u0031',
  assignmentPolicyTitle: '\u5206\u6d3e\u7b56\u7565\u4e2d\u5fc3',
  assignmentPolicyPreview: '\u9884\u6f14\u5f71\u54cd',
  assignmentPolicyPreviewDone:
    '\u7b56\u7565\u9884\u6f14\u5b8c\u6210\uff1a\u0031\u002f\u0032\u0020\u6761\u53cd\u9988\u4f1a\u53d8\u5316',
  assignmentPolicySave: '\u4fdd\u5b58\u7b56\u7565',
  assignmentPolicySaved: '\u5206\u6d3e\u7b56\u7565\u5df2\u4fdd\u5b58',
  assignmentPolicyRestore: '\u6062\u590d',
  assignmentPolicyRestored: '\u5206\u6d3e\u7b56\u7565\u5df2\u6062\u590d',
  assignmentPolicyOwnerLane:
    '\u0055\u0072\u0067\u0065\u006e\u0074\u0020\u006f\u0070\u0065\u006e\u0020\u0066\u0065\u0065\u0064\u0062\u0061\u0063\u006b\u0020\u006f\u0077\u006e\u0065\u0072\u0020\u006c\u0061\u006e\u0065',
  assignmentPolicySla:
    '\u0055\u0072\u0067\u0065\u006e\u0074\u0020\u006f\u0070\u0065\u006e\u0020\u0066\u0065\u0065\u0064\u0062\u0061\u0063\u006b\u0020\u0053\u004c\u0041\u0020\u5c0f\u65f6',
  assignmentPolicyDefaultOwner:
    '\u0055\u0072\u0067\u0065\u006e\u0074\u0020\u006f\u0070\u0065\u006e\u0020\u0066\u0065\u0065\u0064\u0062\u0061\u0063\u006b\u0020\u9ed8\u8ba4\u8d1f\u8d23\u4eba',
  assignmentPolicyNote: '\u53d8\u66f4\u5907\u6ce8',
  assignmentPolicySummary:
    '\u0031\u0020\u6761\u547d\u4e2d\u0020\u0065\u006e\u0074\u0065\u0072\u0070\u0072\u0069\u0073\u0065\u005f\u0074\u0072\u0069\u0061\u0067\u0065\uff0c\u5efa\u8bae\u0020\u0038\u0068\u0020\u0053\u004c\u0041',
  batchAssign: '\u6279\u91cf\u5206\u6d3e',
  batchAssignTitle: '\u6279\u91cf\u5206\u6d3e\u53cd\u9988',
  batchAssignClearOwner: '\u6e05\u7a7a\u8d1f\u8d23\u4eba',
  batchAssignSla: '\u0053\u004c\u0041',
  batchAssignSetSla: '\u8bbe\u7f6e\u0020\u0053\u004c\u0041',
  batchAssignDueAt: '\u622a\u6b62\u65f6\u95f4',
  batchAssignNote: '\u4ea4\u63a5\u5907\u6ce8',
  batchAssignApply: '\u5e94\u7528\u5206\u6d3e',
  batchAssignSaved: '\u5df2\u5206\u6d3e\u0020\u0031\u0020\u6761\u53cd\u9988',
  batchRecommend: '\u63a8\u8350\u5206\u6d3e',
  batchRecommendTitle: '\u63a8\u8350\u5206\u6d3e\u7b56\u7565',
  batchRecommendOwner: '\u5b9e\u9645\u8d1f\u8d23\u4eba',
  batchRecommendNote: '\u5e94\u7528\u5907\u6ce8',
  batchRecommendApply: '\u5e94\u7528\u5efa\u8bae',
  batchRecommendSaved:
    '\u5df2\u5e94\u7528\u0020\u0031\u0020\u6761\u5efa\u8bae\uff0c\u8df3\u8fc7\u0020\u0030\u0020\u6761',
  batchOperator: '\u6279\u91cf\u6307\u6325\u9762',
  batchOperatorTitle: '\u6279\u91cf\u64cd\u4f5c\u6307\u6325\u9762',
  batchOperatorLink: '\u004c\u0069\u006e\u006b\u0020\u9700\u6c42',
  batchOperatorDismiss: '\u6279\u91cf\u5173\u95ed',
  batchOperatorNotify: '批量预览通知',
  batchNotifyTitle: '批量通知 Customer Request 订阅者',
  batchNotifyPreview: '预览 audience',
  batchNotifyPublish: '发布并通知',
  batchNotifyPreviewCounts: '2 个需求 · 4 位可发送 · 1 位已排除 · 1 个失败',
  batchNotifyPartial: '批量操作部分成功',
  batchNotifySucceeded: '成功 1 条',
  batchNotifyFailed: '失败 1 条',
  batchOperatorRecovery: '\u5931\u8d25\u9879\u6062\u590d',
  batchDismissSaved: '\u5df2\u5173\u95ed\u0020\u0031\u0020\u6761\u53cd\u9988',
  portalInbox: '\u95e8\u6237\u6536\u4ef6\u7bb1',
  gdpr: '\u0047\u0044\u0050\u0052\u0020\u6570\u636e\u8bf7\u6c42',
  gdprRetentionWorkflow: 'Retention / legal hold 工作流',
  gdprRetentionFingerprint: '30d audit / 1h delete grace / legal hold off / 2 request records',
  gdprRetentionSummary: '1 retention and legal-hold checks are blocked',
  gdprRetentionPolicy: 'audit 30d / export 1d / prune 1h',
  gdprRetentionLegalHold: 'legal hold off / 1 scheduled deletes',
  gdprRetentionDeleteGrace: 'grace 1h / 1 scheduled deletes / 1 visible',
  gdprRetentionExportResidue: '1 ready exports / expires 2026-06-25T09:00:00Z / TTL 1d',
  gdprRetentionBackupResidue: 'hashed audit on / backup residue on / audit 30d',
  gdprSubjectRequired:
    '\u8bf7\u8f93\u5165\u0020\u0073\u0075\u0062\u006a\u0065\u0063\u0074\u0020\u006b\u0065\u0079',
  mcpGrantsTable: '\u004d\u0043\u0050\u0020\u5237\u65b0\u6388\u6743\u5217\u8868',
  mcpGrantRevoked: '\u5237\u65b0\u6388\u6743\u5df2\u64a4\u9500',
  mcpClients: '\u004d\u0043\u0050\u0020\u5ba2\u6237\u7aef',
  mcpSessionsTable: '\u004d\u0043\u0050\u0020\u4f1a\u8bdd\u5217\u8868',
  mcpSessionRevoked: '\u4f1a\u8bdd\u5df2\u7ec8\u6b62',
  mcpToolsSaved: '\u5de5\u5177\u7b56\u7565\u5df2\u4fdd\u5b58',
  openNav: '\u6253\u5f00\u5bfc\u822a',
  outboxDead: '\u6b7b\u4fe1\u6295\u9012',
  outboxRetryQueued:
    '\u5df2\u91cd\u65b0\u6392\u961f\uff0c\u0077\u006f\u0072\u006b\u0065\u0072\u0020\u5c06\u5728\u4e0b\u6b21\u8f6e\u8be2\u65f6\u6295\u9012\u3002',
  publicVisibility: '\u516c\u5f00\u53ef\u89c1\u6027',
  publicPrivacyPreflight: '公开隐私预检',
  publicPrivacyPreflightAccess: 'public / 4 public surfaces / search off',
  publicPrivacyPreflightFingerprint:
    'public / 4 public surfaces / 2 moderation subjects / 1 portal fields',
  publicPrivacyPreflightGate: '1 pending / 1 approved / request pending / comment pending',
  publicPrivacyPreflightIdentity: 'identity display_name / submitter on / timestamps visible',
  publicPrivacyPreflightRecovery: '0 blocked / 0 reasoned / 2 subjects',
  publicPrivacyPreflightSubmission: '1 fields / 1 required / page URL on',
  publicPrivacyPreflightSummary: '4 privacy preflight checks need attention',
  requestNotifications: '\u9700\u6c42\u901a\u77e5',
  requestNotificationStatusEvidence: '\u72b6\u6001\u901a\u77e5\u8bc1\u636e',
  requestNotificationRecoveryPending: '\u5f85\u6062\u590d\u5ba2\u6237',
  retry: '\u91cd\u8bd5',
  retryDelivery: '\u91cd\u8bd5\u6295\u9012',
  retryEnrichment: '\u91cd\u8bd5\u5bcc\u5316',
  revoke: '\u64a4\u9500',
  revokeGrant: '\u64a4\u9500\u6388\u6743',
  revokeKeyDialog: '\u64a4\u9500\u8fd9\u628a\u0020\u006b\u0065\u0079\uff1f',
  revokeSession: '\u7ec8\u6b62',
  saveToolPolicies: '\u4fdd\u5b58\u5de5\u5177\u7b56\u7565',
  searchFeedback: '\u641c\u7d22\u53cd\u9988\u5185\u5bb9',
  semanticFallbackTitle: '\u8bed\u4e49\u641c\u7d22\u5df2\u964d\u7ea7',
  semanticSearch: '\u8bed\u4e49',
  security: '安全设置',
  securityGovernance: '治理 / RBAC 就绪度',
  securityGovernanceAccessReview: '1 member audit events / 1 actors',
  securityGovernanceBreakglass: 'hybrid / 1 active break-glass / 0 lockouts',
  securityGovernanceFingerprint: 'hybrid / 2 active members / 1 admins / 1 member audit events',
  securityGovernanceIdp: '0 IdP-managed / 2 active members / 0 pending invites',
  securityGovernanceLastAdmin: '1 active admins',
  securityGovernanceRoles: '2 roles represented / 2 active members',
  securityGovernanceSummary: '1 governance readiness checks are blocked',
  securityFieldPermissions: '字段级权限台账',
  securityFieldPermissionsAudit: '1 moderation audit events / 1 actors',
  securityFieldPermissionsFingerprint:
    '4 roles / public / 2 moderation subjects / 1 moderation audit events',
  securityFieldPermissionsModeration: '1 pending / 1 approved / 0 blocked / 2 subjects',
  securityFieldPermissionsProjection: 'public / search off / request pending / comment pending',
  securityFieldPermissionsRoles: '4 roles / 2 policy editors / viewer 3 grants',
  securityFieldPermissionsSummary: '3 field-level permission checks need attention',
  securityFieldPermissionsWrite:
    'submission identified / comments disabled / votes anonymous / identity display_name',
  securityCompliancePackage: '合规包证据',
  securityComplianceAudit: '2 audit events / 1 actors / 2 action types',
  securityComplianceDataFlow: '4 public surfaces / identity display_name / moderation 2 subjects',
  securityComplianceFingerprint:
    'hybrid / 2 active members / 4 public surfaces / 1 outbound targets / 2 audit events',
  securityComplianceRetention: 'audit 30d / legal hold off / 1 scheduled deletes',
  securityComplianceSubprocessor: '1 enabled outbound / 0 failing / 1 HTTPS',
  securityComplianceSummary: '1 compliance package checks are blocked',
  securityKeyRotation: '密钥轮换就绪度',
  securityKeyRotationApi: '1 active keys / 0 expiring / 0 in grace / 1 never expires',
  securityKeyRotationFingerprint:
    '1 active API keys / 1 webhook sources / 1 managed LLM keys / 2 outbound targets / 2 keyset checks',
  securityKeyRotationInbound: '1 webhook sources / 1 enabled / 0 failing',
  securityKeyRotationLlm: '1 LLM channels / 1 managed keys / 1 tested / 0 failing',
  securityKeyRotationOutbound: '1 notify targets / reply hook on / 1 delivery failures',
  securityKeyRotationSummary: '2 key rotation checks need attention',
  securityKeyRotationTink: '2 keyset checks / 2 passing / 0 warning',
  securityWebhookSignature: 'Webhook signature 验证工具',
  securityWebhookSignatureExternal:
    '2 connections / 1 webhook secrets / 1 verified events / 0 signature failures',
  securityWebhookSignatureFailure: '2 signature-path failures / 2 diagnostics / 2 replay paths',
  securityWebhookSignatureFingerprint:
    '1 inbound webhooks / reply hook on / 1 request webhooks / 1 external sync secrets / 0 signature failures',
  securityWebhookSignatureInbound: '1 inbound webhooks / 1 enabled / 0 failing',
  securityWebhookSignatureReply: 'reply hook on / fingerprint on / 1 deliveries / 1 failing',
  securityWebhookSignatureRequest: '1 request webhooks / 1 signed / 0 tested / 0 webhook failures',
  securityWebhookSignatureSummary: '4 webhook signature checks need attention',
  securityIncidentRunbook: '安全事件响应 runbook',
  securityIncidentRunbookAccess: 'hybrid / 1 admins / 0 IdP-managed / 1 member audit events',
  securityIncidentRunbookCredential:
    '1 API keys / 1 managed LLM keys / 2 keyset checks / 1 break-glass',
  securityIncidentRunbookCustomer:
    '1 outbound targets / 1 request failures / 1 reply failures / 0 target failures',
  securityIncidentRunbookFingerprint:
    '1 API keys / 0 signature failures / 1 admins / 4 public surfaces / 2 notification failures',
  securityIncidentRunbookPrivacy:
    '4 public surfaces / 1 pending / legal hold off / 1 scheduled deletes',
  securityIncidentRunbookSummary: '5 security incident lanes need rehearsal',
  securityIncidentRunbookWebhook:
    '0 signature failures / 1 reply failures / 0 request webhook failures / 1 external failures',
  runSemanticSearch: '\u8fd0\u884c\u8bed\u4e49\u641c\u7d22',
  markVerified: '\u6807\u8bb0\u5df2\u9a8c\u8bc1',
  evidenceLabel: '\u5339\u914d\u4f9d\u636e',
  externalSync: '\u5916\u90e8\u540c\u6b65',
  integrationCatalog: '\u96c6\u6210\u76ee\u5f55',
  integrationCatalogCoverage:
    '8 catalog cards / Jira, GitHub, Intercom, Zendesk, Salesforce, HubSpot, Custom webhook, CSV',
  integrationCatalogFingerprint:
    '8/8 connectors / 8 install states / 8 permission maps / 8 sample replays / 8 upgrade paths / verifier on',
  integrationCatalogHealth: '8 health badges / 1 unhealthy tenant connector',
  integrationCatalogInstall: '1 live installs / 8 catalog states / 0 setup blockers',
  integrationCatalogPermissions: '8 permission maps / 23 scopes / least privilege on',
  integrationCatalogReplay: '8 replay fixtures / 8 normalized samples',
  integrationCatalogSummary: '1 integration catalog lanes need hardening',
  integrationCatalogUpgrade: '8 upgrade paths / rollback 8/8',
  upgradeDiagnostics: '\u5347\u7ea7\u8bca\u65ad',
  upgradeDiagnosticsFingerprint:
    '6/6 checks / verifier on / playbook available / compatibility available / fixtures 3/3',
  upgradeDiagnosticsHealth: '1 installed / 1 degraded / 1 quarantined / 1 retryable',
  upgradeDiagnosticsPermissions: '2 scoped connections / 8 permission maps / 0 blank scopes',
  upgradeDiagnosticsReplay: '8 catalog replays / fixture lane verified / 1 received events',
  upgradeDiagnosticsSchema: '5 provider fields / 0 drift risks / mapping v3',
  upgradeDiagnosticsSummary: '1 upgrade diagnostics lanes need hardening',
  upgradeDiagnosticsVersion: '8 connectors / rollback 8/8 / fixtures 3/3',
  upgradeDiagnosticsWebhook: '1 verified / 0 failed / 1 configured secrets',
  connectorConformanceGate: 'Connector conformance gate',
  connectorConformanceFingerprint:
    '1/1 providers / 3/3 fixtures / 6/6 hooks / 1 live connectors / 1 verified signatures',
  connectorConformanceSummary: 'connector conformance is verified',
  connectorConformanceManifest: '1 active live connectors / 1 webhook secrets configured',
  connectorConformanceReplay: '1 received events / 0 replayed in tenant ledger',
  connectorConformanceSignature: '1 verified / 0 failed / 1 configured secrets',
  connectorConformanceMapping: '3 mapped fields / 5 provider fields / 0 problems',
  connectorConformanceRecovery: '3 retryable / 1 quarantined / 0 unauthorized / 1 throttled',
  fieldMappingWorkbench: '\u5b57\u6bb5\u6620\u5c04\u5de5\u4f5c\u53f0',
  fieldMappingFingerprint:
    'GitHub Issues A11y / external-sync-mapping-a11y / 2/2 required fields / 5 provider fields / 0 drift risks',
  fieldMappingSummary: '1 field mapping lanes need hardening',
  fieldMappingSchema: 'schema diff available / 5 fields / 3 writable / 0 required missing',
  fieldMappingRequired: '2/2 required mapped / 0 suggested / 0 drifted / JSON valid',
  fieldMappingStatus: '2/2 required statuses / JSON valid / conflict manual',
  fieldMappingPreview:
    'preview available / impact available / reset available / backfill available',
  fieldMappingRecovery:
    'conflict manual / tombstone mark_stale / reset available / 1 failed records / 1 conflicts',
  fieldMappingTitleSuggestion: '\u5efa\u8bae\u0020title',
  fieldMappingExternalKeySuggestion: '\u5efa\u8bae\u0020number',
  showScopes: '\u67e5\u770b\u751f\u6548\u6743\u9650',
  submitApiKey: '\u65b0\u5efa',
  secretDialog: '\u4f60\u7684\u65b0\u0020\u006b\u0065\u0079',
  signApiKey: '\u7b7e\u53d1\u65b0\u0020\u006b\u0065\u0079',
  signApiKeyDialog: '\u7b7e\u53d1\u65b0\u0020\u0041\u0050\u0049\u0020\u006b\u0065\u0079',
  stepUp: '\u5b8c\u6210\u4e8c\u6b21\u9a8c\u8bc1',
  stepUpConfirm: '\u9a8c\u8bc1\u5e76\u7ee7\u7eed',
  stepUpDialog: '\u786e\u8ba4\u654f\u611f\u64cd\u4f5c',
  stepUpSuccess: '\u4e8c\u6b21\u9a8c\u8bc1\u5df2\u5b8c\u6210',
  startProcessing: '\u5f00\u59cb\u5904\u7406',
  surveys: '\u6ee1\u610f\u5ea6\u8c03\u67e5',
  surveyAccountFilter: '\u8d26\u6237\u6807\u8bc6',
  surveyAccountChip: '\u8d26\u6237\u0020\u0041\u0063\u006d\u0065\u0020\u0043\u006f\u0072\u0070',
  terminalFailures: '\u7ec8\u6001\u5931\u8d25\u5de5\u4f4d',
  useLabel: '\u7528\u9014\u5907\u6ce8',
  viewDetails: '\u67e5\u770b\u8be6\u60c5',
  denyTool: '\u963b\u6b62\u5de5\u5177',
  exportZip: '\u5bfc\u51fa\u0020\u005a\u0049\u0050',
  replyDraft: '\u56de\u590d\u8349\u7a3f',
  replyDraftAi: '\u0041\u0049\u0020\u5efa\u8bae',
  replyDraftHuman: '\u4eba\u5de5\u7f16\u8f91',
  replyDraftSentText: '\u5df2\u53d1\u9001\u6587\u672c',
  replyDraftEdited: '\u5df2\u7f16\u8f91',
  replyDraftApprovedStatus: '\u5df2\u6279\u51c6',
  replyDraftSentStatus: '\u5df2\u53d1\u9001',
  replyDraftEdit: '\u7f16\u8f91',
  replyDraftEditor: '\u56de\u590d\u8349\u7a3f\u7f16\u8f91\u5668',
  replyDraftSave: '\u4fdd\u5b58',
  replyDraftSaveAria: '\u4fdd\u5b58\u56de\u590d\u8349\u7a3f',
  replyDraftSaved: '\u8349\u7a3f\u5df2\u4fdd\u5b58',
  replyDraftApprove: '\u6279\u51c6',
  replyDraftApproved: '\u8349\u7a3f\u5df2\u6279\u51c6',
  replyDraftSend: '\u53d1\u9001',
  replyDraftSent: '\u56de\u590d\u5df2\u53d1\u9001',
  replyDraftHistory: '\u7f16\u8f91\u5386\u53f2',
  replyDraftPreflight: '\u786e\u8ba4\u53d1\u9001\u56de\u590d',
  replyDraftConfirmSend: '\u786e\u8ba4\u53d1\u9001',
  replyDraftEvidence: '\u8bc1\u636e',
  replyDraftFinalText: '\u6700\u7ec8\u53d1\u9001\u6587\u672c',
  replyDraftDiff: '\u53d8\u66f4\u5bf9\u6bd4',
  replySendHook: '\u56de\u590d\u53d1\u9001\u0020\u0048\u006f\u006f\u006b',
  replySendHookSaved:
    '\u56de\u590d\u53d1\u9001\u0020\u0048\u006f\u006f\u006b\u0020\u5df2\u4fdd\u5b58',
  replySendHookDisabled:
    '\u56de\u590d\u53d1\u9001\u0020\u0048\u006f\u006f\u006b\u0020\u5df2\u505c\u7528',
  replySendHookDisable: '\u505c\u7528\u0020\u0048\u006f\u006f\u006b',
  replySendHookDisableConfirm: '\u786e\u8ba4\u505c\u7528',
  replySendHookDisableDialog:
    '\u505c\u7528\u56de\u590d\u53d1\u9001\u0020\u0048\u006f\u006f\u006b\uff1f',
  replySendHookName: '\u540d\u79f0',
  replySendHookContract: '\u6295\u9012\u5951\u7ea6',
  replySendHookDelivery: '\u6700\u8fd1\u6295\u9012',
  replySendHookHealthAttention: '\u6700\u8fd1\u6295\u9012\u9700\u8981\u5904\u7406',
  replySendHookSecurity: '\u5b89\u5168\u68c0\u67e5',
  replySendHookTest: '\u6d4b\u8bd5\u0020\u0048\u006f\u006f\u006b',
  replySendHookTestAccepted:
    '\u6d4b\u8bd5\u6295\u9012\u5df2\u88ab\u0020\u0048\u006f\u006f\u006b\u0020\u63a5\u6536',
  replySendHookRedeliver: '\u91cd\u653e\u6295\u9012',
  replySendHookRedelivered: '\u6295\u9012\u5df2\u91cd\u653e\u5e76\u88ab\u63a5\u6536',
  replySendHookPayloadLabel:
    '\u56de\u590d\u53d1\u9001\u0020\u0048\u006f\u006f\u006b\u0020\u793a\u4f8b\u0020\u0070\u0061\u0079\u006c\u006f\u0061\u0064',
  replySendHookOneTimeSecret: '\u4e00\u6b21\u6027\u0020\u0073\u0065\u0063\u0072\u0065\u0074',
  replySendHookURLHttpsError: '回复发送 Hook 只接受 HTTPS URL；本地测试可使用 loopback HTTP。',
  webhookUrl: '\u0057\u0065\u0062\u0068\u006f\u006f\u006b\u0020\u0055\u0052\u004c',
}

const apiKeyCreateFailure = 'Create key failed for accessibility gate'
const apiKeyRevokeFailure = 'Revoke key failed for accessibility gate'
const feedbackRetryFailure = 'Retry enrichment failed for accessibility gate'
const mcpToolPolicyFailure = 'Tool policy save failed for accessibility gate'
const outboxRetryFailure = 'Retry delivery conflict for accessibility gate'
const badRequestResourceConsole =
  'Failed to load resource: the server responded with a status of 400'
const conflictResourceConsole = 'Failed to load resource: the server responded with a status of 409'
const serverErrorResourceConsole =
  'Failed to load resource: the server responded with a status of 500'

const routes = [
  { path: '/control-tower', title: zh.controlTower, heading: zh.controlTower },
  {
    path: '/analytics/classification-quality',
    title: zh.classificationQuality,
    heading: zh.classificationQuality,
  },
  { path: '/analytics/anomalies', title: zh.anomalies, heading: zh.anomalies },
  {
    path: '/configuration/anomaly-detection',
    title: zh.anomalies,
    heading: zh.anomalies,
  },
  { path: '/feedback', title: zh.feedback, heading: zh.feedback },
  {
    path: '/feedback/customer-requests',
    title: zh.customerRequests,
    heading: zh.customerRequests,
  },
  {
    path: '/feedback/terminal-failures',
    title: zh.terminalFailures,
    heading: zh.terminalFailures,
  },
  { path: '/integrations/api-keys', title: zh.apiKeys, heading: zh.apiKeys },
  { path: '/integrations/external-sync', title: zh.externalSync, heading: zh.externalSync },
  {
    path: '/integrations/reply-send-hook',
    title: zh.replySendHook,
    heading: zh.replySendHook,
  },
  {
    path: '/integrations/public-visibility',
    title: zh.publicVisibility,
    heading: zh.publicVisibility,
  },
  {
    path: '/integrations/request-notifications',
    title: zh.requestNotifications,
    heading: zh.requestNotifications,
  },
  { path: '/integrations/surveys', title: zh.surveys, heading: zh.surveys },
  { path: '/mcp-clients', title: zh.mcpClients, heading: zh.mcpClients },
  { path: '/administration/gdpr', title: zh.gdpr, heading: zh.gdpr },
  { path: '/administration/security', title: zh.security, heading: zh.security },
  { path: '/administration/reliability', title: zh.reliability, heading: zh.reliability },
  { path: '/administration/dead-deliveries', title: zh.outboxDead, heading: zh.outboxDead },
  { path: '/administration/audit-log', title: zh.auditLog, heading: zh.auditLog },
] as const

const narrowMobileRoutes = [
  routes[0],
  routes[2],
  routes[6],
  routes[8],
  routes[13],
  routes[16],
] as const

const redirectAliases = [
  {
    path: '/api-keys',
    canonicalPath: '/integrations/api-keys',
    title: zh.apiKeys,
    heading: zh.apiKeys,
  },
  {
    path: '/outbox-dead',
    canonicalPath: '/administration/dead-deliveries',
    title: zh.outboxDead,
    heading: zh.outboxDead,
  },
] as const

const shellNavigationCases = [
  {
    linkName: zh.mcpClients,
    canonicalPath: '/mcp-clients',
    title: zh.mcpClients,
    heading: zh.mcpClients,
  },
  {
    linkName: zh.outboxDead,
    canonicalPath: '/administration/dead-deliveries',
    title: zh.outboxDead,
    heading: zh.outboxDead,
  },
] as const

const stressWidths = [320, 280] as const
const routeChurnCycles = 3
const sheetCycles = 3
const wcagTextSpacingStyle = `
  * {
    line-height: 1.5 !important;
    letter-spacing: 0.12em !important;
    word-spacing: 0.16em !important;
  }

  p {
    margin-bottom: 2em !important;
  }
`
const textZoomStyle = `
  html {
    font-size: 200% !important;
  }
`

test.describe('Console accessibility browser gate', () => {
  for (const routeCase of routes) {
    test(`${routeCase.path} has a clean accessible render`, async ({ page }) => {
      const diagnostics = collectConsoleDiagnostics(page)
      const apiMocks = await installConsoleApiMocks(page)

      await gotoConsoleRoute(page, routeCase.path)

      await expect(page).toHaveTitle(new RegExp(`${escapeRegExp(routeCase.title)}.*Attune Console`))
      await expect(page.getByRole('heading', { level: 1, name: routeCase.heading })).toBeVisible()
      await expectNoDocumentOverflow(page)
      await expectNoAxeViolations(page)
      expect(apiMocks.unhandledRequests).toEqual([])
      await expectNoConsoleDiagnostics(diagnostics)
    })
  }

  test('control tower closed-loop scorecard stays actionable in the browser', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/control-tower')

    await expect(page.getByRole('heading', { level: 1, name: zh.controlTower })).toBeVisible()
    await expect(page.getByText(zh.closedLoopRecovery).first()).toBeVisible()
    await expect(page.getByText('54/100').first()).toBeVisible()
    await expect(page.getByText(zh.closedLoopProof)).toBeVisible()
    const firstValue = page.getByTestId('control-tower-first-value')
    await expect(firstValue.getByText(zh.firstValueActivation)).toBeVisible()
    await expect(firstValue.getByText(zh.firstValueProgress)).toBeVisible()
    await expect(firstValue.getByText(zh.sourceConnected)).toBeVisible()
    await expect(firstValue.getByText(zh.signalCaptured)).toBeVisible()
    const sourceHealth = page.getByTestId('control-tower-source-health')
    await expect(sourceHealth.getByText(zh.sourceHealth)).toBeVisible()
    await expect(sourceHealth.getByText(zh.sourceHealthNext)).toBeVisible()
    await expect(sourceHealth.getByText(zh.sourceHealthSource)).toBeVisible()
    const sourceErrorProblem = page.getByTestId('control-tower-source-problem-errors')
    await expect(sourceErrorProblem).toBeVisible()
    await expect(sourceErrorProblem.getByText(zh.sourceHealthError)).toBeVisible()
    const recoveryCommand = page.getByTestId('control-tower-recovery-command')
    await expect(recoveryCommand.getByText(zh.recoveryCommand)).toBeVisible()
    await expect(recoveryCommand.getByText(zh.recoveryCommandNext)).toBeVisible()
    await expect(recoveryCommand.getByText(zh.recoveryOwner)).toBeVisible()
    await expect(page.getByTestId('control-tower-recovery-blocker-overdue')).toBeVisible()
    const readinessMatrix = page.getByTestId('control-tower-readiness-matrix')
    await expect(readinessMatrix.getByText(zh.readinessMatrix)).toBeVisible()
    await expect(readinessMatrix.getByText(zh.signalQualityTrust)).toBeVisible()
    await expect(readinessMatrix.getByText(zh.releaseVerification)).toBeVisible()
    const releaseVerification = page.getByTestId('control-tower-release-verification')
    await expect(releaseVerification.getByText(zh.releaseEvidence)).toBeVisible()
    await expect(releaseVerification.getByText(zh.releaseEvidenceContract)).toBeVisible()
    await expect(releaseVerification.getByText(zh.releaseEvidenceProductContract)).toBeVisible()
    const worldClassMaturity = page.getByTestId('control-tower-world-class-maturity')
    await expect(worldClassMaturity.getByText(zh.worldClassMaturity)).toBeVisible()
    await expect(worldClassMaturity.getByText(zh.worldClassMaturityGap)).toBeVisible()
    await expect(worldClassMaturity.getByText(zh.worldClassMaturityIdentity).first()).toBeVisible()
    await expect(worldClassMaturity.getByText(zh.worldClassMaturityAccount).first()).toBeVisible()
    await expect(worldClassMaturity.getByText(zh.worldClassExecution)).toBeVisible()
    await expect(worldClassMaturity.getByText(zh.worldClassExecutionCount)).toBeVisible()
    await expect(worldClassMaturity.getByText(zh.worldClassExecutionNorthStarMetrics)).toBeVisible()
    await expect(
      page
        .getByTestId('control-tower-readiness-signal-quality')
        .getByText(zh.blockedStatus, { exact: true }),
    ).toBeVisible()
    await expect(
      page
        .getByTestId('control-tower-readiness-release-verification')
        .getByText(zh.passStatus, { exact: true }),
    ).toBeVisible()

    const recoveryAction = page.getByTestId('control-tower-risk-closed-loop-recovery')
    await expect(
      recoveryAction.getByRole('heading', { name: zh.closedLoopRecoveryRisk }),
    ).toBeVisible()
    await expect(recoveryAction.getByText(zh.actionOpen, { exact: true })).toBeVisible()

    await recoveryAction.getByRole('button', { name: zh.startProcessing }).click()
    await expect(recoveryAction.getByText(zh.actionInProgress, { exact: true })).toBeVisible()

    await recoveryAction.getByRole('button', { name: zh.markVerified }).click()
    await expect(recoveryAction.getByText(zh.actionVerified, { exact: true })).toBeVisible()

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('classification quality exposes AI review learning evidence in the browser', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/analytics/classification-quality')

    await expect(
      page.getByRole('heading', { level: 1, name: zh.classificationQuality }),
    ).toBeVisible()
    await expect(page.getByText(zh.classificationReviewLearning)).toBeVisible()
    await expect(page.getByText(zh.classificationTrainingCandidates)).toBeVisible()
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('survey low-score recovery account filter narrows rows in the browser', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/surveys')

    await expect(page.getByRole('heading', { level: 1, name: zh.surveys })).toBeVisible()
    await expect(page.getByText(zh.surveyAccountChip)).toBeVisible()
    const filteredResponse = page.waitForResponse((response) => {
      const url = new URL(response.url())
      return (
        url.pathname.endsWith('/fb/v1/console/surveys/responses') &&
        url.searchParams.get('account_key') === 'acct:acme'
      )
    })
    await page.getByLabel(zh.surveyAccountFilter).fill('acct:acme')
    await filteredResponse
    await expect(page.getByText(zh.surveyAccountChip)).toBeVisible()

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('request notification status evidence is visible in the browser', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/request-notifications')

    await expect(
      page.getByRole('heading', { level: 1, name: zh.requestNotifications }),
    ).toBeVisible()
    await expect(
      page.getByText(zh.requestNotificationRecoveryPending, { exact: true }),
    ).toBeVisible()
    const evidence = page.getByTestId('request-notification-status-evidence')
    await expect(evidence.getByText(zh.requestNotificationStatusEvidence)).toBeVisible()
    await expect(evidence.getByTestId('rn-status-evidence-shipped')).toBeVisible()

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('reliability pipeline, consistency, backup, quota, incident, release, replay, and error-budget ledgers are visible in the browser', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/administration/reliability')

    await expect(page.getByRole('heading', { level: 1, name: zh.reliability })).toBeVisible()
    const releaseHealthLedger = page.getByTestId('reliability-release-health-ledger')
    await expect(releaseHealthLedger.getByText(zh.reliabilityReleaseHealthLedger)).toBeVisible()
    await expect(releaseHealthLedger.getByText('5d6ea83 / production / supported')).toBeVisible()
    await expect(releaseHealthLedger.getByText('1 urgent / 2 total feedback')).toBeVisible()
    await expect(
      releaseHealthLedger.getByText('1 failed / 1 recovery pending customers'),
    ).toBeVisible()
    const incidentTimeline = page.getByTestId('reliability-incident-timeline')
    await expect(incidentTimeline.getByText(zh.reliabilityIncidentTimeline)).toBeVisible()
    await expect(incidentTimeline.getByText('A11y Tenant / 5d6ea83 / supported')).toBeVisible()
    await expect(
      incidentTimeline.getByText('4 incident timeline phases need attention'),
    ).toBeVisible()
    await expect(incidentTimeline.getByText('worker: Replay queue needs review')).toBeVisible()
    await expect(
      incidentTimeline.getByText('1 failed / 1 recovery pending customers'),
    ).toBeVisible()
    const tenantQuota = page.getByTestId('reliability-tenant-quota-saturation')
    await expect(tenantQuota.getByText(zh.reliabilityTenantQuota)).toBeVisible()
    await expect(tenantQuota.getByText('1 quota lanes are saturated')).toBeVisible()
    await expect(tenantQuota.getByText('72% used / 1 unbounded active API keys')).toBeVisible()
    await expect(tenantQuota.getByText('1 unbounded / 1 active MCP clients').first()).toBeVisible()
    await expect(tenantQuota.getByText('20% dead-letter saturation')).toBeVisible()
    const backupRestore = page.getByTestId('reliability-backup-restore-drill')
    await expect(backupRestore.getByText(zh.reliabilityBackupRestore)).toBeVisible()
    await expect(backupRestore.getByText('A11y Tenant / nightly-backup / supported')).toBeVisible()
    await expect(backupRestore.getByText('backup and restore evidence is verified')).toBeVisible()
    await expect(
      backupRestore.getByText('backup=nightly-backup / age=1h / window=7d'),
    ).toBeVisible()
    await expect(backupRestore.getByText('pass restore / 1.2s')).toBeVisible()
    const consistency = page.getByTestId('reliability-consistency-checks')
    await expect(consistency.getByText(zh.reliabilityConsistency)).toBeVisible()
    await expect(
      consistency.getByText('A11y Tenant / 2 feedback / 1 requests / 2 survey completions'),
    ).toBeVisible()
    await expect(consistency.getByText('2 consistency checks need attention')).toBeVisible()
    await expect(
      consistency.getByText('72 ingested / 2 feedback records / 1 usage buckets'),
    ).toBeVisible()
    await expect(
      consistency.getByText('2 supporting feedback / 1 requests / 0 orphaned requests'),
    ).toBeVisible()
    await expect(
      consistency.getByText('2 notified / 4 expected / 1 request statuses'),
    ).toBeVisible()
    await expect(consistency.getByText('1 low-score / 1 open reviews / 1 overdue')).toBeVisible()
    const pipelineSlo = page.getByTestId('reliability-pipeline-slo-ledger')
    await expect(pipelineSlo.getByText(zh.reliabilityPipelineSlo)).toBeVisible()
    await expect(
      pipelineSlo.getByText('A11y Tenant / 72 ingested / 20 enrich calls / 1 sync rows'),
    ).toBeVisible()
    await expect(pipelineSlo.getByText('3 pipeline SLOs need attention')).toBeVisible()
    await expect(pipelineSlo.getByText('72 ingested / 1 buckets / 72.0% quota used')).toBeVisible()
    await expect(pipelineSlo.getByText('20 calls / 1 errors / 5.0% error rate')).toBeVisible()
    await expect(
      pipelineSlo.getByText('1 synced / 0 stale / 0 failed / 0 pending / 0 manual'),
    ).toBeVisible()
    const replayDrill = page.getByTestId('reliability-replay-drill')
    await expect(replayDrill.getByText(zh.reliabilityReplayDrill)).toBeVisible()
    await expect(replayDrill.getByText(zh.reliabilityReplayOutbox)).toBeVisible()
    await expect(
      replayDrill.getByText('Retry dead deliveries and verify destination acceptance'),
    ).toBeVisible()
    const errorBudgetLedger = page.getByTestId('reliability-error-budget-ledger')
    await expect(errorBudgetLedger.getByText(zh.reliabilityErrorBudgetLedger)).toBeVisible()
    await expect(errorBudgetLedger.getByText(zh.reliabilityReplayOutbox)).toBeVisible()
    await expect(
      errorBudgetLedger.getByText('attune:ingest_service_failure_ratio:ratio5m / 0.001'),
    ).toBeVisible()
    await expect(errorBudgetLedger.getByText('0.10% budget allowance').first()).toBeVisible()
    await expect(page.getByText('Replay 工作区')).toBeVisible()

    await expectNoAxeViolations(page)
    await expectNoDocumentOverflow(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('developer API adoption kit is visible in the browser', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/api-keys')

    await expect(page.getByRole('heading', { level: 1, name: zh.apiKeys })).toBeVisible()
    const adoptionKit = page.getByTestId('developer-api-adoption-kit')
    await expect(adoptionKit.getByText(zh.developerAdoptionKit)).toBeVisible()
    await expect(adoptionKit.getByText(zh.developerAdoptionFingerprint)).toBeVisible()
    await expect(adoptionKit.getByText(zh.developerAdoptionSummary)).toBeVisible()
    await expect(adoptionKit.getByText(zh.developerAdoptionOpenApi)).toBeVisible()
    await expect(adoptionKit.getByText(zh.developerAdoptionNode)).toBeVisible()
    await expect(adoptionKit.getByText(zh.developerAdoptionGo)).toBeVisible()
    await expect(adoptionKit.getByText(zh.developerAdoptionSandbox)).toBeVisible()
    await expect(adoptionKit.getByText(zh.developerAdoptionReplay)).toBeVisible()
    const sdkParityGate = page.getByTestId('developer-sdk-parity-gate')
    await expect(sdkParityGate.getByText(zh.developerSdkParityGate)).toBeVisible()
    await expect(sdkParityGate.getByText(zh.developerSdkParityFingerprint)).toBeVisible()
    await expect(sdkParityGate.getByText(zh.developerSdkParitySummary)).toBeVisible()
    await expect(sdkParityGate.getByText(zh.developerSdkParityManagement)).toBeVisible()
    await expect(sdkParityGate.getByText(zh.developerSdkParityError)).toBeVisible()
    await expect(sdkParityGate.getByText(zh.developerSdkParityRetry)).toBeVisible()
    await expect(sdkParityGate.getByText(zh.developerSdkParityBrowser)).toBeVisible()
    await expect(sdkParityGate.getByText(zh.developerSdkParityRelease)).toBeVisible()
    const apiConsistencyContract = page.getByTestId('developer-api-consistency-contract')
    await expect(apiConsistencyContract.getByText(zh.developerApiConsistencyContract)).toBeVisible()
    await expect(
      apiConsistencyContract.getByText(zh.developerApiConsistencyFingerprint),
    ).toBeVisible()
    await expect(apiConsistencyContract.getByText(zh.developerApiConsistencySummary)).toBeVisible()
    await expect(
      apiConsistencyContract.getByText(zh.developerApiConsistencyPagination),
    ).toBeVisible()
    await expect(apiConsistencyContract.getByText(zh.developerApiConsistencyFilter)).toBeVisible()
    await expect(apiConsistencyContract.getByText(zh.developerApiConsistencySort)).toBeVisible()
    await expect(apiConsistencyContract.getByText(zh.developerApiConsistencyError)).toBeVisible()
    await expect(
      apiConsistencyContract.getByText(zh.developerApiConsistencyIdempotency),
    ).toBeVisible()
    await expect(apiConsistencyContract.getByText(zh.developerApiConsistencyWire)).toBeVisible()
    const importExportWorkbench = page.getByTestId('developer-import-export-workbench')
    await expect(importExportWorkbench.getByText(zh.developerImportExportWorkbench)).toBeVisible()
    await expect(importExportWorkbench.getByText(zh.developerImportExportFingerprint)).toBeVisible()
    await expect(importExportWorkbench.getByText(zh.developerImportExportSummary)).toBeVisible()
    await expect(importExportWorkbench.getByText(zh.developerImportExportTemplate)).toBeVisible()
    await expect(importExportWorkbench.getByText(zh.developerImportExportSchema)).toBeVisible()
    await expect(importExportWorkbench.getByText(zh.developerImportExportMapping)).toBeVisible()
    await expect(importExportWorkbench.getByText(zh.developerImportExportDryRun)).toBeVisible()
    await expect(importExportWorkbench.getByText(zh.developerImportExportRecovery)).toBeVisible()
    await expect(importExportWorkbench.getByText(zh.developerImportExportGovernance)).toBeVisible()

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoAxeViolations(page)
    await expectNoDocumentOverflow(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('external sync connector conformance gate is visible in the browser', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/external-sync')

    await expect(page.getByRole('heading', { level: 1, name: zh.externalSync })).toBeVisible()
    const integrationCatalog = page.getByTestId('external-sync-integration-catalog')
    await expect(integrationCatalog.getByText(zh.integrationCatalog)).toBeVisible()
    await expect(integrationCatalog.getByText(zh.integrationCatalogFingerprint)).toBeVisible()
    await expect(integrationCatalog.getByText(zh.integrationCatalogSummary)).toBeVisible()
    await expect(
      integrationCatalog
        .getByTestId('external-sync-integration-catalog-catalog_cards')
        .getByText(zh.integrationCatalogCoverage),
    ).toBeVisible()
    await expect(
      integrationCatalog
        .getByTestId('external-sync-integration-catalog-install_status')
        .getByText(zh.integrationCatalogInstall),
    ).toBeVisible()
    await expect(
      integrationCatalog
        .getByTestId('external-sync-integration-catalog-permission_scope')
        .getByText(zh.integrationCatalogPermissions),
    ).toBeVisible()
    await expect(
      integrationCatalog
        .getByTestId('external-sync-integration-catalog-health_badge')
        .getByText(zh.integrationCatalogHealth),
    ).toBeVisible()
    await expect(
      integrationCatalog
        .getByTestId('external-sync-integration-catalog-sample_replay')
        .getByText(zh.integrationCatalogReplay),
    ).toBeVisible()
    await expect(
      integrationCatalog
        .getByTestId('external-sync-integration-catalog-upgrade_path')
        .getByText(zh.integrationCatalogUpgrade),
    ).toBeVisible()
    const upgradeDiagnostics = page.getByTestId('external-sync-upgrade-diagnostics')
    await expect(upgradeDiagnostics.getByText(zh.upgradeDiagnostics, { exact: true })).toBeVisible()
    await expect(upgradeDiagnostics.getByText(zh.upgradeDiagnosticsFingerprint)).toBeVisible()
    await expect(upgradeDiagnostics.getByText(zh.upgradeDiagnosticsSummary)).toBeVisible()
    await expect(
      upgradeDiagnostics
        .getByTestId('external-sync-upgrade-diagnostics-install_health')
        .getByText(zh.upgradeDiagnosticsHealth),
    ).toBeVisible()
    await expect(
      upgradeDiagnostics
        .getByTestId('external-sync-upgrade-diagnostics-permission_boundary')
        .getByText(zh.upgradeDiagnosticsPermissions),
    ).toBeVisible()
    await expect(
      upgradeDiagnostics
        .getByTestId('external-sync-upgrade-diagnostics-schema_drift')
        .getByText(zh.upgradeDiagnosticsSchema),
    ).toBeVisible()
    await expect(
      upgradeDiagnostics
        .getByTestId('external-sync-upgrade-diagnostics-webhook_readiness')
        .getByText(zh.upgradeDiagnosticsWebhook),
    ).toBeVisible()
    await expect(
      upgradeDiagnostics
        .getByTestId('external-sync-upgrade-diagnostics-fixture_replay')
        .getByText(zh.upgradeDiagnosticsReplay),
    ).toBeVisible()
    await expect(
      upgradeDiagnostics
        .getByTestId('external-sync-upgrade-diagnostics-version_compatibility')
        .getByText(zh.upgradeDiagnosticsVersion),
    ).toBeVisible()
    const conformanceGate = page.getByTestId('external-sync-connector-conformance-gate')
    await expect(conformanceGate.getByText(zh.connectorConformanceGate)).toBeVisible()
    await expect(conformanceGate.getByText(zh.connectorConformanceFingerprint)).toBeVisible()
    await expect(conformanceGate.getByText(zh.connectorConformanceSummary)).toBeVisible()
    await expect(
      conformanceGate
        .getByTestId('external-sync-connector-conformance-gate-connector_manifest')
        .getByText(zh.connectorConformanceManifest),
    ).toBeVisible()
    await expect(
      conformanceGate
        .getByTestId('external-sync-connector-conformance-gate-fixture_replay')
        .getByText(zh.connectorConformanceReplay),
    ).toBeVisible()
    await expect(
      conformanceGate
        .getByTestId('external-sync-connector-conformance-gate-webhook_signature')
        .getByText(zh.connectorConformanceSignature),
    ).toBeVisible()
    await expect(
      conformanceGate
        .getByTestId('external-sync-connector-conformance-gate-field_mapping')
        .getByText(zh.connectorConformanceMapping),
    ).toBeVisible()
    await expect(
      conformanceGate
        .getByTestId('external-sync-connector-conformance-gate-error_recovery')
        .getByText(zh.connectorConformanceRecovery),
    ).toBeVisible()

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoAxeViolations(page)
    await expectNoDocumentOverflow(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('external sync field mapping workbench is visible in the browser', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/external-sync')

    await expect(page.getByRole('heading', { level: 1, name: zh.externalSync })).toBeVisible()
    const workbench = page.getByTestId('external-sync-field-mapping-workbench')
    await expect(workbench.getByText(zh.fieldMappingWorkbench)).toBeVisible()
    await expect(workbench.getByText(zh.fieldMappingFingerprint)).toBeVisible()
    await expect(workbench.getByText(zh.fieldMappingSummary)).toBeVisible()
    await expect(
      workbench
        .getByTestId('external-sync-field-mapping-workbench-schema_diff')
        .getByText(zh.fieldMappingSchema),
    ).toBeVisible()
    await expect(
      workbench
        .getByTestId('external-sync-field-mapping-workbench-required_mapping')
        .getByText(zh.fieldMappingRequired),
    ).toBeVisible()
    await expect(
      workbench
        .getByTestId('external-sync-field-mapping-workbench-status_mapping')
        .getByText(zh.fieldMappingStatus),
    ).toBeVisible()
    await expect(
      workbench
        .getByTestId('external-sync-field-mapping-workbench-preview_backfill')
        .getByText(zh.fieldMappingPreview),
    ).toBeVisible()
    await expect(
      workbench
        .getByTestId('external-sync-field-mapping-workbench-rollback_recovery')
        .getByText(zh.fieldMappingRecovery),
    ).toBeVisible()
    await expect(
      workbench
        .getByTestId('external-sync-field-mapping-row-title')
        .getByText(zh.fieldMappingTitleSuggestion),
    ).toBeVisible()
    await expect(
      workbench
        .getByTestId('external-sync-field-mapping-row-external_key')
        .getByText(zh.fieldMappingExternalKeySuggestion),
    ).toBeVisible()

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoAxeViolations(page)
    await expectNoDocumentOverflow(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('security governance and field-level permission evidence is visible in the browser', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/administration/security')

    await expect(page.getByRole('heading', { level: 1, name: zh.security })).toBeVisible()
    const governance = page.getByTestId('security-governance-rbac-readiness')
    await expect(governance.getByText(zh.securityGovernance)).toBeVisible()
    await expect(governance.getByText(zh.securityGovernanceFingerprint)).toBeVisible()
    await expect(governance.getByText(zh.securityGovernanceSummary)).toBeVisible()
    await expect(governance.getByText(zh.securityGovernanceBreakglass)).toBeVisible()
    await expect(governance.getByText(zh.securityGovernanceIdp)).toBeVisible()
    await expect(governance.getByText(zh.securityGovernanceRoles)).toBeVisible()
    await expect(
      governance.getByText(zh.securityGovernanceLastAdmin, { exact: true }),
    ).toBeVisible()
    await expect(governance.getByText(zh.securityGovernanceAccessReview)).toBeVisible()
    const fieldPermissions = page.getByTestId('security-field-level-permissions-ledger')
    await expect(fieldPermissions.getByText(zh.securityFieldPermissions)).toBeVisible()
    await expect(fieldPermissions.getByText(zh.securityFieldPermissionsFingerprint)).toBeVisible()
    await expect(fieldPermissions.getByText(zh.securityFieldPermissionsSummary)).toBeVisible()
    await expect(fieldPermissions.getByText(zh.securityFieldPermissionsRoles)).toBeVisible()
    await expect(fieldPermissions.getByText(zh.securityFieldPermissionsProjection)).toBeVisible()
    await expect(fieldPermissions.getByText(zh.securityFieldPermissionsWrite)).toBeVisible()
    await expect(fieldPermissions.getByText(zh.securityFieldPermissionsModeration)).toBeVisible()
    await expect(fieldPermissions.getByText(zh.securityFieldPermissionsAudit)).toBeVisible()
    const compliancePackage = page.getByTestId('security-compliance-package-evidence')
    await expect(compliancePackage.getByText(zh.securityCompliancePackage)).toBeVisible()
    await expect(compliancePackage.getByText(zh.securityComplianceFingerprint)).toBeVisible()
    await expect(compliancePackage.getByText(zh.securityComplianceSummary)).toBeVisible()
    await expect(compliancePackage.getByText(zh.securityComplianceDataFlow)).toBeVisible()
    await expect(compliancePackage.getByText(zh.securityComplianceAudit)).toBeVisible()
    await expect(compliancePackage.getByText(zh.securityComplianceRetention)).toBeVisible()
    await expect(compliancePackage.getByText(zh.securityComplianceSubprocessor)).toBeVisible()
    const keyRotation = page.getByTestId('security-key-rotation-readiness')
    await expect(keyRotation.getByText(zh.securityKeyRotation)).toBeVisible()
    await expect(keyRotation.getByText(zh.securityKeyRotationFingerprint)).toBeVisible()
    await expect(keyRotation.getByText(zh.securityKeyRotationSummary)).toBeVisible()
    await expect(keyRotation.getByText(zh.securityKeyRotationTink)).toBeVisible()
    await expect(keyRotation.getByText(zh.securityKeyRotationApi)).toBeVisible()
    await expect(
      keyRotation.getByText(zh.securityKeyRotationInbound, { exact: true }),
    ).toBeVisible()
    await expect(keyRotation.getByText(zh.securityKeyRotationOutbound)).toBeVisible()
    await expect(keyRotation.getByText(zh.securityKeyRotationLlm)).toBeVisible()
    const webhookSignature = page.getByTestId('security-webhook-signature-tooling')
    await expect(webhookSignature.getByText(zh.securityWebhookSignature)).toBeVisible()
    await expect(webhookSignature.getByText(zh.securityWebhookSignatureFingerprint)).toBeVisible()
    await expect(webhookSignature.getByText(zh.securityWebhookSignatureSummary)).toBeVisible()
    await expect(
      webhookSignature.getByText(zh.securityWebhookSignatureInbound, { exact: true }),
    ).toBeVisible()
    await expect(webhookSignature.getByText(zh.securityWebhookSignatureReply)).toBeVisible()
    await expect(webhookSignature.getByText(zh.securityWebhookSignatureRequest)).toBeVisible()
    await expect(webhookSignature.getByText(zh.securityWebhookSignatureExternal)).toBeVisible()
    await expect(webhookSignature.getByText(zh.securityWebhookSignatureFailure)).toBeVisible()
    const incidentRunbook = page.getByTestId('security-incident-runbook')
    await expect(incidentRunbook.getByText(zh.securityIncidentRunbook)).toBeVisible()
    await expect(incidentRunbook.getByText(zh.securityIncidentRunbookFingerprint)).toBeVisible()
    await expect(incidentRunbook.getByText(zh.securityIncidentRunbookSummary)).toBeVisible()
    await expect(incidentRunbook.getByText(zh.securityIncidentRunbookCredential)).toBeVisible()
    await expect(incidentRunbook.getByText(zh.securityIncidentRunbookWebhook)).toBeVisible()
    await expect(incidentRunbook.getByText(zh.securityIncidentRunbookAccess)).toBeVisible()
    await expect(incidentRunbook.getByText(zh.securityIncidentRunbookPrivacy)).toBeVisible()
    await expect(incidentRunbook.getByText(zh.securityIncidentRunbookCustomer)).toBeVisible()

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoAxeViolations(page)
    await expectNoDocumentOverflow(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('public privacy preflight evidence is visible in the browser', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/public-visibility')

    await expect(page.getByRole('heading', { level: 1, name: zh.publicVisibility })).toBeVisible()
    const preflight = page.getByTestId('public-privacy-preflight')
    await expect(preflight.getByText(zh.publicPrivacyPreflight)).toBeVisible()
    await expect(preflight.getByText(zh.publicPrivacyPreflightFingerprint)).toBeVisible()
    await expect(preflight.getByText(zh.publicPrivacyPreflightSummary)).toBeVisible()
    await expect(preflight.getByText(zh.publicPrivacyPreflightAccess)).toBeVisible()
    await expect(preflight.getByText(zh.publicPrivacyPreflightGate)).toBeVisible()
    await expect(preflight.getByText(zh.publicPrivacyPreflightIdentity)).toBeVisible()
    await expect(preflight.getByText(zh.publicPrivacyPreflightSubmission)).toBeVisible()
    await expect(preflight.getByText(zh.publicPrivacyPreflightRecovery)).toBeVisible()

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoAxeViolations(page)
    await expectNoDocumentOverflow(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  for (const routeCase of redirectAliases) {
    test(`${routeCase.path} redirects to its canonical accessible page`, async ({ page }) => {
      const diagnostics = collectConsoleDiagnostics(page)
      const apiMocks = await installConsoleApiMocks(page)

      await gotoConsoleRoute(page, routeCase.path)

      await expect(page).toHaveURL(new RegExp(`/console${escapeRegExp(routeCase.canonicalPath)}$`))
      await expect(page).toHaveTitle(new RegExp(`${escapeRegExp(routeCase.title)}.*Attune Console`))
      await expect(page.getByRole('heading', { level: 1, name: routeCase.heading })).toBeVisible()
      await expectNoDocumentOverflow(page)
      await expectNoAxeViolations(page)
      expect(apiMocks.unhandledRequests).toEqual([])
      await expectNoConsoleDiagnostics(diagnostics)
    })
  }

  for (const routeCase of shellNavigationCases) {
    test(`shell navigation reaches ${routeCase.canonicalPath} with accessible state intact`, async ({
      page,
    }) => {
      const diagnostics = collectConsoleDiagnostics(page)
      const apiMocks = await installConsoleApiMocks(page)

      await gotoConsoleRoute(page, '/feedback')
      await clickShellNav(page, routeCase.linkName)

      await expect(page).toHaveURL(new RegExp(`/console${escapeRegExp(routeCase.canonicalPath)}$`))
      await expect(page).toHaveTitle(new RegExp(`${escapeRegExp(routeCase.title)}.*Attune Console`))
      await expect(page.getByRole('heading', { level: 1, name: routeCase.heading })).toBeVisible()
      await expectNoDocumentOverflow(page)
      await expectNoAxeViolations(page)
      expect(apiMocks.unhandledRequests).toEqual([])
      await expectNoConsoleDiagnostics(diagnostics)
    })
  }

  test('critical routes stay contained at narrow mobile widths', async ({ page }) => {
    test.setTimeout(180_000)
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    for (const width of stressWidths) {
      await page.setViewportSize({ width, height: 568 })

      for (const routeCase of narrowMobileRoutes) {
        await gotoConsoleRoute(page, routeCase.path)
        await expectRouteShellState(page, routeCase)
        await expectNoDocumentOverflow(page)
        await expectNoAxeViolations(page)
      }
    }

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('critical routes preserve accessible structure in forced-colors mode', async ({ page }) => {
    test.setTimeout(120_000)
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.emulateMedia({ forcedColors: 'active' })

    for (const routeCase of routes) {
      await gotoConsoleRoute(page, routeCase.path)
      await expectRouteShellState(page, routeCase)
      await expectNoDocumentOverflow(page)
      await expectNoAxeViolations(page)
    }

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('critical routes tolerate WCAG text-spacing overrides', async ({ page }) => {
    test.setTimeout(120_000)
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })

    for (const routeCase of routes) {
      await gotoConsoleRoute(page, routeCase.path)
      await page.addStyleTag({ content: wcagTextSpacingStyle })
      await expectRouteShellState(page, routeCase)
      await expectNoDocumentOverflow(page)
      await expectNoAxeViolations(page)
    }

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('critical routes preserve reflow with 200 percent text sizing', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1280, height: 720 })

    for (const routeCase of routes) {
      await gotoConsoleRoute(page, routeCase.path)
      await page.addStyleTag({ content: textZoomStyle })
      await expectRouteShellState(page, routeCase)
      await expectNoDocumentOverflow(page)
    }

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('route churn keeps shell landmarks, titles, and dialogs clean', async ({ page }) => {
    test.setTimeout(120_000)
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 320, height: 568 })

    for (let cycle = 0; cycle < routeChurnCycles; cycle += 1) {
      for (const routeCase of routes) {
        await gotoConsoleRoute(page, `${routeCase.path}?routeChurn=${cycle}`)
        await expectRouteShellState(page, routeCase)
        await expectNoDocumentOverflow(page)
      }
    }

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('GDPR subject input contains long identifiers at narrow width', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 320, height: 568 })
    await gotoConsoleRoute(page, '/administration/gdpr')

    const subjectKey = `edge-${'x'.repeat(480)}@example.com`
    const subjectInput = page.getByTestId('gdpr-subject-key')
    await subjectInput.fill(subjectKey)

    await expect(subjectInput).toHaveValue(subjectKey)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('GDPR retention and legal hold evidence is visible in the browser', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/administration/gdpr')

    const workflow = page.getByTestId('gdpr-retention-legal-hold-workflow')
    await expect(workflow.getByText(zh.gdprRetentionWorkflow)).toBeVisible()
    await expect(workflow.getByText(zh.gdprRetentionFingerprint)).toBeVisible()
    await expect(workflow.getByText(zh.gdprRetentionSummary)).toBeVisible()
    await expect(workflow.getByText(zh.gdprRetentionPolicy)).toBeVisible()
    await expect(workflow.getByText(zh.gdprRetentionLegalHold)).toBeVisible()
    await expect(workflow.getByText(zh.gdprRetentionDeleteGrace)).toBeVisible()
    await expect(workflow.getByText(zh.gdprRetentionExportResidue)).toBeVisible()
    await expect(workflow.getByText(zh.gdprRetentionBackupResidue)).toBeVisible()

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoAxeViolations(page)
    await expectNoDocumentOverflow(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('API key dialog cycles restore focus without leaking dialogs', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 320, height: 568 })
    await gotoConsoleRoute(page, '/integrations/api-keys')

    const createButton = page.getByRole('button', {
      name: new RegExp(escapeRegExp(zh.signApiKey)),
    })

    for (let cycle = 0; cycle < 6; cycle += 1) {
      await createButton.click()
      await expect(
        page.getByRole('dialog', { name: new RegExp(escapeRegExp(zh.signApiKeyDialog)) }),
      ).toBeVisible()
      if (cycle === 0) {
        await expectNoAxeViolations(page)
      }
      await expectNoDocumentOverflow(page)

      await page.keyboard.press('Escape')
      await expect(page.getByRole('dialog')).toHaveCount(0)
      await expectDomFocus(createButton)
      await expectNoDocumentOverflow(page)
    }

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback detail sheet survives repeated open-close cycles', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 320, height: 568 })

    await gotoConsoleRoute(page, '/feedback')
    await expect(page.getByText(zh.feedbackTriageCommand)).toBeVisible()
    await expect(page.getByText('support_triage').first()).toBeVisible()
    await expect(page.getByText(zh.identityReviewTitle)).toBeVisible()
    await expect(page.getByText(zh.identityReviewCandidate)).toBeVisible()
    await expect(page.getByText(zh.identityReviewNeedsEvidence)).toBeVisible()
    await expect(page.getByText(zh.identityReviewSubjects).first()).toBeVisible()
    await expect(page.getByText('grace@example.com')).toBeVisible()
    await page.getByRole('button', { name: /grace@example.com/ }).click()
    await expect(page.getByText(zh.identityReviewSubjectDetail, { exact: true })).toBeVisible()
    await expect(page.getByText(zh.identityReviewTimeline)).toBeVisible()
    await expect(page.getByText(zh.identityReviewRevoked)).toBeVisible()
    await expect(page.getByText('Support ID - zd-42').first()).toBeVisible()
    await expect(
      page.getByText('Grace asked for audit-ready identity review evidence.'),
    ).toBeVisible()
    await page.getByRole('button', { name: zh.identityReviewApproveMerge }).click()
    await expect(page.getByText(zh.identityReviewMergeSuccess)).toBeVisible()
    await page.getByRole('button', { name: zh.identityReviewUndoMerge }).click()
    await expect(page.getByText(zh.identityReviewSplitSuccess)).toBeVisible()
    const feedbackOpeners = page.getByRole('button', { name: /#feedback-101/ })
    expect(await feedbackOpeners.count()).toBeGreaterThan(0)
    await feedbackOpeners.nth(0).click()
    const sheet = page.getByRole('dialog')
    await expect(sheet.getByText(zh.identityEvidence)).toBeVisible()
    await expect(sheet.getByText(zh.identityCandidateCount)).toBeVisible()
    await expect(sheet.getByText(zh.identityResolutionTitle)).toBeVisible()
    await expect(sheet.getByText(zh.identityResolutionAction)).toBeVisible()
    await expect(sheet.getByText(zh.identityResolutionCounts)).toBeVisible()
    await expect(sheet.getByText('ada@example.com', { exact: true })).toBeVisible()
    await expect(sheet.getByText('source_meta.contact.sourceContactId')).toBeVisible()
    await expect(sheet.getByText(zh.signalTrace)).toBeVisible()
    await expect(sheet.getByText('trace-feedback-101').first()).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(sheet).toHaveCount(0)
    await cycleSheet(page, feedbackOpeners.nth(0), sheetCycles)

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('customer requests account filter scopes the request list', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 390, height: 844 })
    await gotoConsoleRoute(page, '/feedback/customer-requests')

    const requestTitle = 'Restore focus after closing detail panels'
    await expect(page.getByRole('heading', { level: 1, name: zh.customerRequests })).toBeVisible()
    await expect(page.getByText(requestTitle)).toBeVisible()

    await page.getByRole('button', { name: new RegExp(escapeRegExp(requestTitle)) }).click()
    const detailSheet = page.getByRole('dialog')
    await expect(detailSheet.getByText('Acme Corp')).toBeVisible()
    await expect(detailSheet.getByText(zh.customerRequestsDecisionBreakdown)).toBeVisible()
    await expect(detailSheet.getByText(zh.customerRequestsDecisionBreakdownTotal)).toBeVisible()
    await expect(detailSheet.getByText(zh.customerRequestsDecisionFactorRevenue)).toBeVisible()
    await expect(detailSheet.getByText(zh.customerRequestsEvidenceQuality).first()).toBeVisible()
    await expect(detailSheet.getByText(zh.customerRequestsEvidenceQualityStrength)).toBeVisible()
    await expect(detailSheet.getByText(zh.customerRequestsDecisionRecords)).toBeVisible()
    await expect(detailSheet.getByText('Updated customer request')).toBeVisible()
    await expect(detailSheet.getByText(zh.customerRequestsDecisionRecordStatus)).toBeVisible()
    await expect(detailSheet.getByText(zh.customerRequestsDecisionOwner)).toBeVisible()
    await expect(detailSheet.getByText(zh.customerRequestsDecisionRationale)).toBeVisible()
    await expect(detailSheet.getByText(zh.customerRequestsDecisionEvidenceBundle)).toBeVisible()
    await expect(detailSheet.getByText(zh.customerRequestsDecisionPublicSafe)).toBeVisible()
    await expect(detailSheet.getByText(zh.customerRequestsDecisionPublicReason)).toBeVisible()
    await detailSheet.getByRole('button', { name: zh.customerRequestsAccountView }).click()
    await expect(detailSheet).toHaveCount(0)
    await expect.poll(() => new URL(page.url()).searchParams.get('account_key')).toBe('acct:acme')

    const accountFilter = page.getByLabel(zh.customerRequestsAccountFilter)
    await expect(accountFilter).toHaveValue('acct:acme')
    const accountOverview = page.getByTestId('customer-request-account-overview')
    await expect(accountOverview.getByText(zh.customerRequestsAccountOverview)).toBeVisible()
    await expect(accountOverview.getByText(zh.customerRequestsAccountOverviewScope)).toBeVisible()
    await expect(accountOverview.getByText(zh.customerRequestsAccountOverviewProfile)).toBeVisible()
    await expect(
      accountOverview.getByText(zh.customerRequestsAccountOverviewRequests),
    ).toBeVisible()
    await expect(
      accountOverview.getByText(zh.customerRequestsAccountOverviewRevenue, { exact: true }),
    ).toBeVisible()
    await expect(
      accountOverview.getByText(zh.customerRequestsAccountOverviewAverageScore),
    ).toBeVisible()
    await expect(
      accountOverview.getByText(zh.customerRequestsAccountOverviewTopScore),
    ).toBeVisible()
    await expect(accountOverview.getByText(zh.customerRequestsAccountOverviewSignals)).toBeVisible()
    await expect(
      accountOverview.getByText(zh.customerRequestsAccountOverviewSignalHighPriority),
    ).toBeVisible()
    await expect(
      accountOverview.getByText(zh.customerRequestsAccountOverviewSignalRevenue),
    ).toBeVisible()
    await expect(
      accountOverview.getByText(zh.customerRequestsAccountOverviewSignalEvidence),
    ).toBeVisible()
    await expect(accountOverview.getByText(zh.customerRequestsAccountOverviewEvents)).toBeVisible()
    await expect(
      accountOverview.getByText(zh.customerRequestsAccountEventFeedbackLinked),
    ).toBeVisible()
    await expect(
      accountOverview.getByText(zh.customerRequestsAccountEventIssueSynced),
    ).toBeVisible()
    await expect(accountOverview.getByText(zh.customerRequestsAccountEventNoteAdded)).toBeVisible()
    await expect(accountOverview.getByText('Keyboard users lose context')).toBeVisible()
    await expect(accountOverview.getByText('ATT-42')).toBeVisible()
    await expect(
      accountOverview.getByText(zh.customerRequestsAccountOverviewTimeline),
    ).toBeVisible()

    await accountFilter.fill('acct:missing')
    await expect(page.getByText(zh.customerRequestsEmpty)).toBeVisible()
    await expect(page.getByText(requestTitle)).toHaveCount(0)

    await accountFilter.fill('acct:acme')
    await expect(
      page.getByRole('button', {
        name: new RegExp(`CR-1.*Open.*High.*${escapeRegExp(requestTitle)}`),
      }),
    ).toBeVisible()

    expect(
      apiMocks.customerRequestListRequests.some(
        (search) => new URLSearchParams(search).get('account_key') === 'acct:acme',
      ),
    ).toBe(true)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('portal inbox renders escaped HTML-like submission text in the detail sheet', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback/portal')

    await expect(page.getByRole('heading', { level: 1, name: zh.portalInbox })).toBeVisible()
    await expect(page.getByText('门户提交').first()).toBeVisible()

    await page
      .getByRole('button', { name: /#feedback-301/ })
      .first()
      .click()

    const portalHeading = page.getByRole('heading', { name: '门户投稿' })
    await expect(portalHeading).toBeVisible()
    const portalSection = portalHeading.locator('xpath=ancestor::section[1]')
    await expect(portalSection).toBeVisible()

    const portalHTML = await portalSection.evaluate((element) => element.innerHTML)
    expect(portalHTML).toContain('&lt;img src=x onerror="window.__portalXssTitle=1"&gt;')
    expect(portalHTML).toContain('&lt;svg onload="window.__portalXssDetails=1"&gt;&lt;/svg&gt;')
    expect(portalHTML).toContain('&lt;b&gt;Ada&lt;/b&gt;')
    expect(portalHTML).toContain('&lt;em&gt;xss&lt;/em&gt;')
    expect(portalHTML).toContain('&lt;script&gt;alert(1)&lt;/script&gt;')
    expect(portalHTML).toContain('PortalTest/1.0')

    await expect(portalSection.getByText('<b>Ada</b>')).toBeVisible()
    await expect(portalSection.getByText('<em>xss</em>')).toBeVisible()
    await expect(portalSection.getByText(/<script>alert\(1\)<\/script>/)).toBeVisible()
    await expect(portalSection.locator('img')).toHaveCount(0)
    const readPortalXssMarker = (marker: 'title' | 'details') =>
      page.evaluate((key) => {
        const portalWindow = window as Window & {
          __portalXssDetails?: unknown
          __portalXssTitle?: unknown
        }
        if (key === 'title') return portalWindow.__portalXssTitle ?? null
        return portalWindow.__portalXssDetails ?? null
      }, marker)

    await expect.poll(() => readPortalXssMarker('title')).toBeNull()
    await expect.poll(() => readPortalXssMarker('details')).toBeNull()
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback tags do not offer duplicate create CTA for assigned names', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')

    await page
      .getByRole('button', { name: /#feedback-101/ })
      .first()
      .click()
    const sheet = page.getByRole('dialog')
    await expect(sheet).toContainText('Focus lost after detail close')
    await expect(sheet.getByRole('button', { name: '移除标签 accessibility' })).toBeVisible()

    await sheet.getByRole('button', { name: '添加' }).click()
    const tagSearch = page.getByPlaceholder('搜索标签…')
    await expect(tagSearch).toBeVisible()
    await tagSearch.fill('accessibility')
    const listbox = page.getByRole('listbox')
    await expect(listbox).toContainText('标签「accessibility」已添加到当前反馈')
    await expect(page.getByRole('option', { name: '创建「accessibility」' })).toHaveCount(0)

    await tagSearch.press('Escape')
    await expect(page.getByRole('listbox')).toHaveCount(0)

    await sheet.getByRole('button', { name: '移除标签 accessibility' }).click()
    await expect(sheet.getByText('暂无标签')).toBeVisible()

    await sheet.getByRole('button', { name: '添加' }).click()
    await page.getByPlaceholder('搜索标签…').fill('accessibility')
    await expect(page.getByRole('option', { name: 'accessibility' })).toBeVisible()
    await expect(page.getByRole('option', { name: '创建「accessibility」' })).toHaveCount(0)
    await page.getByRole('option', { name: 'accessibility' }).click()
    await expect(sheet.getByRole('button', { name: '移除标签 accessibility' })).toBeVisible()

    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback account filter narrows rows and stays visible in detail', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')

    const accountFilteredFeedback = page.waitForResponse((response) => {
      const url = new URL(response.url())
      return (
        url.pathname === '/fb/v1/console/feedback' &&
        url.searchParams.get('account_key') === 'acct:acme'
      )
    })
    await page.getByRole('textbox', { name: zh.feedbackAccountFilter }).fill('acct:acme')
    await accountFilteredFeedback
    await expect(page.getByText(zh.feedbackAccountChip).first()).toBeVisible()

    await page
      .getByRole('button', { name: /#feedback-101/ })
      .first()
      .click()
    const sheet = page.getByRole('dialog')
    await expect(sheet.getByText(zh.feedbackAccountContext).first()).toBeVisible()
    await expect(sheet.getByText('acct:acme').first()).toBeVisible()

    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback assignment owner and SLA can be saved from the detail sheet', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')
    await page
      .getByRole('button', { name: /#feedback-101/ })
      .first()
      .click()

    const sheet = page.getByRole('dialog')
    await expect(sheet.getByText(zh.assignmentTitle)).toBeVisible()
    const ownerSelect = sheet.getByRole('combobox', { name: zh.assignmentOwner })
    await expect(ownerSelect).toContainText('ops@example.com')
    await ownerSelect.click()
    await page.getByRole('option', { name: 'pm@example.com' }).click()
    await sheet.getByLabel(zh.assignmentDue).fill('2026-06-26T09:30')
    await sheet.getByLabel(zh.assignmentNote).fill('Reassign to PM for release readiness.')
    await sheet.getByRole('button', { name: zh.assignmentSave }).click()

    await expectToastStatus(page, zh.assignmentSaved)
    await expect(ownerSelect).toContainText('pm@example.com')
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback batch assignment owner and SLA can be saved from the selection bar', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')
    await page.getByRole('checkbox', { name: /Focus lost after detail close/ }).click()
    await page.getByRole('button', { name: zh.batchAssign }).click()

    const dialog = page.getByRole('dialog', { name: zh.batchAssignTitle })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('combobox', { name: zh.assignmentOwner }).click()
    await page.getByRole('option', { name: zh.batchAssignClearOwner }).click()
    await dialog.getByRole('combobox', { name: zh.batchAssignSla }).click()
    await page.getByRole('option', { name: zh.batchAssignSetSla }).click()
    await dialog.getByLabel(zh.batchAssignDueAt).fill('2026-06-26T09:30')
    await dialog.getByLabel(zh.batchAssignNote).fill('Batch handoff for release readiness.')
    await dialog.getByRole('button', { name: zh.batchAssignApply }).click()

    await expectToastStatus(page, zh.batchAssignSaved)
    await expect(dialog).toBeHidden()
    await expectAppShellVisibleToAxe(page)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback batch command center exposes operator actions and dismiss recovery', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')
    await page.getByRole('checkbox', { name: /Focus lost after detail close/ }).click()
    await page.getByRole('button', { name: zh.batchOperator }).click()

    let dialog = page.getByRole('dialog', { name: zh.batchOperatorTitle })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByRole('button', { name: zh.batchOperatorLink })).toBeVisible()
    await expect(dialog.getByRole('button', { name: zh.batchAssign })).toBeVisible()
    await expect(dialog.getByRole('button', { name: zh.batchOperatorDismiss })).toBeVisible()
    await expect(dialog.getByRole('button', { name: zh.batchOperatorNotify })).toBeVisible()
    await expect(dialog.getByText(zh.batchOperatorRecovery, { exact: true })).toBeVisible()

    await dialog.getByRole('button', { name: zh.batchOperatorDismiss }).click()
    await expectToastStatus(page, zh.batchDismissSaved)

    await page.getByRole('checkbox', { name: /Focus lost after detail close/ }).click()
    await page.getByRole('button', { name: zh.batchOperator }).click()
    dialog = page.getByRole('dialog', { name: zh.batchOperatorTitle })
    await dialog.getByRole('button', { name: zh.batchOperatorNotify }).click()
    const notifyDialog = page.getByRole('dialog', { name: zh.batchNotifyTitle })
    await expect(notifyDialog).toBeVisible()
    await notifyDialog
      .getByTestId('feedback-batch-notify-request-ids')
      .fill('11111111-1111-1111-1111-111111111111\nbad-request')
    await notifyDialog.getByLabel('标题').fill('Shipped')
    await notifyDialog.getByLabel('正文').fill('CSV export is now available.')
    await notifyDialog.getByRole('button', { name: zh.batchNotifyPreview }).click()
    await expect(notifyDialog.getByText(zh.batchNotifyPreviewCounts)).toBeVisible()
    await notifyDialog.getByRole('button', { name: zh.batchNotifyPublish }).click()
    await expect(page.getByText(zh.batchNotifyPartial)).toBeVisible()
    await expect(page.getByText(zh.batchNotifySucceeded)).toBeVisible()
    await expect(page.getByText(zh.batchNotifyFailed)).toBeVisible()
    await expect(notifyDialog).toBeHidden()
    await expectAppShellVisibleToAxe(page)

    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback assignment recommendations can be previewed and applied from selection', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')
    await page.getByRole('checkbox', { name: /Focus lost after detail close/ }).click()
    await page.getByRole('button', { name: zh.batchRecommend }).click()

    const dialog = page.getByRole('dialog', { name: zh.batchRecommendTitle })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText('Urgent open feedback')).toBeVisible()
    await dialog.getByRole('combobox', { name: zh.batchRecommendOwner }).click()
    await page.getByRole('option', { name: 'pm@example.com' }).click()
    await dialog.getByLabel(zh.batchRecommendNote).fill('Policy sweep for urgent queue.')
    await dialog.getByRole('button', { name: zh.batchRecommendApply }).click()

    await expectToastStatus(page, zh.batchRecommendSaved)
    await expect(dialog).toBeHidden()
    await expectAppShellVisibleToAxe(page)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback assignment escalation queue opens at-risk work', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')
    await expect(page.getByText(zh.assignmentEscalationTitle)).toBeVisible()
    await page.getByRole('button', { name: zh.assignmentEscalationOpenFeedback101 }).click()

    const dialog = page.getByRole('dialog')
    await expect(dialog.getByText('Focus lost after detail close').first()).toBeVisible()
    await expect(dialog.getByText(zh.assignmentTitle)).toBeVisible()

    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback assignment policy edits drive the recommendation preview', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')
    await expect(page.getByText(zh.assignmentPolicyTitle)).toBeVisible()

    await page.getByLabel(zh.assignmentPolicyOwnerLane).fill('enterprise_triage')
    await page.getByLabel(zh.assignmentPolicySla).fill('8')
    await page.getByRole('combobox', { name: zh.assignmentPolicyDefaultOwner }).click()
    await page.getByRole('option', { name: 'pm@example.com' }).click()
    await page.getByLabel(zh.assignmentPolicyNote).fill('Enterprise release policy.')
    await page.getByRole('button', { name: zh.assignmentPolicyPreview }).click()
    await expectToastStatus(page, zh.assignmentPolicyPreviewDone)
    await page.getByRole('button', { name: zh.assignmentPolicySave }).click()
    await expectToastStatus(page, zh.assignmentPolicySaved)

    await page.getByRole('checkbox', { name: /Focus lost after detail close/ }).click()
    await page.getByRole('button', { name: zh.batchRecommend }).click()

    const dialog = page.getByRole('dialog', { name: zh.batchRecommendTitle })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText('Urgent open feedback')).toBeVisible()
    await expect(dialog.getByText(zh.assignmentPolicySummary)).toBeVisible()
    await dialog.getByRole('button', { name: zh.batchRecommendApply }).click()
    await expectToastStatus(page, zh.batchRecommendSaved)
    await expect(dialog).toHaveCount(0)
    await page.getByRole('button', { name: zh.assignmentPolicyRestore }).last().click()
    await expectToastStatus(page, zh.assignmentPolicyRestored)
    await expect(page.getByLabel(zh.assignmentPolicyOwnerLane)).toHaveValue('support_triage')

    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('reply draft review workflow edits, approves, and sends a guarded revision', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')
    await page
      .getByRole('button', { name: /#feedback-101/ })
      .first()
      .click()

    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: zh.replyDraft })).toBeVisible()
    await expect(dialog.getByText(zh.replyDraftAi)).toBeVisible()
    const evidencePanel = page.getByTestId('reply-draft-evidence-panel')
    await expect(evidencePanel).toBeVisible()
    await expect(evidencePanel.getByText(zh.replyDraftEvidence, { exact: true })).toBeVisible()
    await expectOpaqueBackground(page, '[data-slot="sheet-content"]', 'feedback detail sheet')
    await expectOpaqueBackground(page, '[data-testid="reply-draft-surface"]', 'reply draft surface')

    await dialog.getByRole('button', { name: zh.replyDraftEdit }).click()
    const editedReply = 'Human edited reply from the browser gate before approval.'
    await dialog.getByLabel(zh.replyDraftEditor).fill(editedReply)
    await dialog.getByRole('button', { name: zh.replyDraftSaveAria }).click()
    await expectToastStatus(page, zh.replyDraftSaved)
    await expect(dialog.getByText(zh.replyDraftEdited)).toBeVisible()
    await expect(dialog.getByText(zh.replyDraftHuman)).toBeVisible()
    await expect(dialog.getByText(editedReply).first()).toBeVisible()

    await dialog.getByRole('button', { name: zh.replyDraftApprove }).click()
    await expectToastStatus(page, zh.replyDraftApproved)
    await expect(dialog.getByText(zh.replyDraftApprovedStatus, { exact: true })).toBeVisible()

    await dialog.getByRole('button', { name: zh.replyDraftSend }).click()
    const preflight = page.getByRole('dialog', { name: zh.replyDraftPreflight })
    await expect(preflight).toBeVisible()
    await expect(preflight.getByText(zh.replyDraftFinalText)).toBeVisible()
    await expect(preflight.getByText(editedReply)).toBeVisible()
    await preflight.getByRole('button', { name: zh.replyDraftConfirmSend }).click()
    await expectToastStatus(page, zh.replyDraftSent)
    await expect(dialog.getByText(zh.replyDraftSentStatus, { exact: true }).first()).toBeVisible()
    await expect(dialog.getByText(zh.replyDraftSentText)).toBeVisible()
    await expect(dialog.getByText(zh.replyDraftHistory)).toBeVisible()
    await expect(dialog.getByText(zh.replyDraftDiff)).toBeVisible()

    expect(apiMocks.replyDraftRequests).toEqual([
      expect.objectContaining({
        method: 'POST',
        path: '/feedback/feedback-101/reply-draft/edit',
        body: { content: editedReply, expectedRevision: '1' },
      }),
      expect.objectContaining({
        method: 'POST',
        path: '/feedback/feedback-101/reply-draft/approve',
        body: { expectedRevision: '2' },
      }),
      expect.objectContaining({
        method: 'POST',
        path: '/feedback/feedback-101/reply-draft/send',
        body: { expectedRevision: '3' },
        idempotencyKey: expect.stringMatching(/.+/),
      }),
    ])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('reply send hook page saves and disables the controlled send endpoint', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/integrations/reply-send-hook')

    await expectOpaqueBackground(page, 'body', 'document body')
    await expectOpaqueBackground(page, '#root', 'app root')
    await expectOpaqueBackground(page, 'main', 'console main')
    await expect(page.getByRole('heading', { level: 1, name: zh.replySendHook })).toBeVisible()
    await expect(page.getByText('hooks.example.com').first()).toBeVisible()
    await expect(page.getByTestId('reply-send-hook-health')).toBeVisible()
    await expect(page.getByText(zh.replySendHookHealthAttention)).toBeVisible()
    await expect(page.getByTestId('reply-send-hook-contract')).toBeVisible()
    await expect(page.getByText(zh.replySendHookContract)).toBeVisible()
    await expect(page.getByTestId('reply-send-hook-deliveries')).toBeVisible()
    await expect(page.getByText(zh.replySendHookDelivery).first()).toBeVisible()
    await expect(
      page.getByTestId('reply-send-hook-deliveries').getByText('receiver returned 500'),
    ).toBeVisible()
    await expect(page.getByText('X-Attune-Signature')).toBeVisible()
    await expect(page.getByText('X-Attune-Timestamp')).toBeVisible()
    await expect(page.getByLabel(zh.replySendHookPayloadLabel)).toHaveValue(
      /"event_type": "reply\.send"/,
    )
    await expect(page.getByText(zh.replySendHookSecurity)).toBeVisible()
    await page.getByRole('button', { name: zh.replySendHookTest }).first().click()
    await expectToastStatus(page, zh.replySendHookTestAccepted)
    await page.getByRole('button', { name: new RegExp(zh.replySendHookRedeliver) }).click()
    await expectToastStatus(page, zh.replySendHookRedelivered)
    await page.getByLabel(zh.replySendHookName).fill('Browser reply bridge')
    await page.getByLabel(zh.webhookUrl).fill('http://support.example.com/attune/replies')
    await expect(page.getByText(zh.replySendHookURLHttpsError)).toBeVisible()
    await expect(page.getByRole('button', { name: zh.replyDraftSave })).toBeDisabled()
    await page.getByLabel(zh.webhookUrl).fill('http://127.0.0.1:4174/attune/replies')
    await expect(page.getByText(zh.replySendHookURLHttpsError)).toBeHidden()
    await expect(page.getByRole('button', { name: zh.replyDraftSave })).toBeEnabled()
    await page.getByLabel(zh.webhookUrl).fill('https://support.example.com/attune/replies')
    await page.getByRole('button', { name: zh.replyDraftSave }).click()
    await expectToastStatus(page, zh.replySendHookSaved)
    await expect(page.getByText(zh.replySendHookOneTimeSecret, { exact: true })).toBeVisible()
    await expect(page.getByText('generated_reply_secret_a11y_123456')).toBeVisible()

    await page.getByRole('button', { name: zh.replySendHookDisable }).click()
    await expect(
      page.getByRole('alertdialog', { name: zh.replySendHookDisableDialog }),
    ).toBeVisible()
    await page.getByRole('button', { name: zh.replySendHookDisableConfirm }).click()
    await expectToastStatus(page, zh.replySendHookDisabled)
    await expect(
      page.getByRole('alertdialog', { name: zh.replySendHookDisableDialog }),
    ).toHaveCount(0)
    await expect(page.locator('.min-h-screen[aria-hidden="true"]')).toHaveCount(0)
    await expect(page.getByText(zh.replySendHookDisabled)).toBeVisible()

    expect(apiMocks.replySendHookRequests).toEqual([
      expect.objectContaining({
        method: 'POST',
        path: '/reply-send-hook/test',
      }),
      expect.objectContaining({
        method: 'POST',
        path: '/reply-send-hook/deliveries/reply-delivery-a11y-failed/redeliver',
      }),
      expect.objectContaining({
        method: 'PUT',
        path: '/reply-send-hook',
        body: {
          enabled: true,
          name: 'Browser reply bridge',
          url: 'https://support.example.com/attune/replies',
        },
      }),
      expect.objectContaining({
        method: 'DELETE',
        path: '/reply-send-hook',
        body: null,
      }),
    ])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('audit detail sheet survives repeated open-close cycles', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 320, height: 568 })

    await gotoConsoleRoute(page, '/administration/audit-log')
    const auditOpeners = page.getByRole('button', { name: zh.viewDetails })
    expect(await auditOpeners.count()).toBeGreaterThan(0)
    await cycleSheet(page, auditOpeners.nth(0), sheetCycles)

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('terminal workbench jump links target every failure dimension', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 320, height: 568 })
    await gotoConsoleRoute(page, '/feedback/terminal-failures')

    const anchorRail = page.locator('a[href^="#terminal-workbench-"]')
    await expect(anchorRail).toHaveCount(4)

    for (let index = 0; index < 4; index += 1) {
      const anchor = anchorRail.nth(index)
      const href = await anchor.getAttribute('href')
      expect(href).toBeTruthy()

      await anchor.click()
      await expect(page).toHaveURL(new RegExp(`${escapeRegExp(href ?? '')}$`))
      await expect(page.locator(`[id="${(href ?? '').replace(/^#/, '')}"]`)).toBeVisible()
      await expectNoDocumentOverflow(page)
    }

    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('semantic feedback search keeps terminal scope and opens returned details', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 390, height: 844 })
    await gotoConsoleRoute(page, '/feedback/terminal-failures')

    await page.getByRole('button', { name: zh.semanticSearch, exact: true }).click()
    await page.getByRole('searchbox', { name: zh.searchFeedback }).fill('terminal upstream retries')
    await page.getByRole('button', { name: zh.runSemanticSearch }).click()

    await expect(page.getByText('rrf.pgfts.v1.k60')).toBeVisible()
    await expect(page.getByText(zh.evidenceLabel)).toBeVisible()
    await expect(page.getByRole('button', { name: /#feedback-201/ }).first()).toBeVisible()

    const request = apiMocks.semanticSearchRequests.at(-1) as {
      q?: string
      limit?: number
      filter?: { enrichmentStatus?: string; terminalFailedOnly?: boolean }
    }
    expect(request.q).toBe('terminal upstream retries')
    expect(request.limit).toBe(50)
    expect(request.filter?.enrichmentStatus).toBe('failed')
    expect(request.filter?.terminalFailedOnly).toBe(true)

    await page
      .getByRole('button', { name: /#feedback-201/ })
      .first()
      .click()
    await expect(page.getByRole('dialog')).toContainText('Terminal enrichment failure')
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('semantic feedback search fallback state stays visible and contained', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')

    await page.getByRole('button', { name: zh.semanticSearch, exact: true }).click()
    await page.getByRole('searchbox', { name: zh.searchFeedback }).fill('fallback coverage check')
    await page.getByRole('button', { name: zh.runSemanticSearch }).click()

    await expect(page.getByText(zh.semanticFallbackTitle)).toBeVisible()
    await expect(page.getByText(zh.evidenceLabel)).toBeVisible()
    const request = apiMocks.semanticSearchRequests.at(-1) as { q?: string; limit?: number }
    expect(request.q).toBe('fallback coverage check')
    expect(request.limit).toBe(50)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('API key creation dialogs remain accessible through the secret reveal', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/api-keys')
    await page.getByRole('button', { name: new RegExp(escapeRegExp(zh.signApiKey)) }).click()
    await expect(
      page.getByRole('dialog', { name: new RegExp(escapeRegExp(zh.signApiKeyDialog)) }),
    ).toBeVisible()
    const scopeDisclosure = page.getByRole('button', { name: zh.showScopes })
    const effectiveScopes = page.locator('#api-key-effective-scopes')
    await expect(scopeDisclosure).toHaveAttribute('aria-expanded', 'false')
    await expect(scopeDisclosure).toHaveAttribute('aria-controls', 'api-key-effective-scopes')
    await expect(effectiveScopes).toBeHidden()

    await scopeDisclosure.click()
    await expect(scopeDisclosure).toHaveAttribute('aria-expanded', 'true')
    await expect(effectiveScopes).toBeVisible()
    await expect(effectiveScopes).toHaveAccessibleName(zh.effectiveScopes)
    await expectNoAxeViolations(page)

    await page.getByRole('textbox', { name: zh.useLabel }).fill('browser-a11y')
    await page.getByRole('button', { name: zh.submitApiKey }).click()
    const secretDialog = page.getByRole('dialog', {
      name: new RegExp(escapeRegExp(zh.secretDialog)),
    })
    await expect(secretDialog).toBeVisible()
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page, '[role="dialog"]')
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('API key creation errors remain visible and axe-clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page, {
      fail: { apiKeyCreate: apiKeyCreateFailure },
    })

    await gotoConsoleRoute(page, '/integrations/api-keys')
    await page.getByRole('button', { name: new RegExp(escapeRegExp(zh.signApiKey)) }).click()
    await page.getByRole('textbox', { name: zh.useLabel }).fill('browser-a11y')
    await page.getByRole('button', { name: zh.submitApiKey }).click()
    await expectToastStatus(page, apiKeyCreateFailure)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(
      diagnostics,
      ['/fb/v1/console/api-keys'],
      [badRequestResourceConsole, apiKeyCreateFailure],
    )
  })

  test('API key revoke dialog reports success through an accessible status', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/api-keys')
    const revokeButton = page.getByRole('button', { name: /ci-accessibility/ })
    await revokeButton.click()
    const revokeDialog = page.getByRole('dialog', { name: zh.revokeKeyDialog })
    await expect(revokeDialog).toBeVisible()
    await expectNoAxeViolations(page)

    await revokeDialog.getByRole('button', { name: zh.revoke }).click()
    await expect(revokeDialog).toBeHidden()
    await expectToastStatus(page, zh.apiKeyRevoked)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('API key revoke errors remain visible and axe-clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page, {
      fail: { apiKeyRevoke: apiKeyRevokeFailure },
    })

    await gotoConsoleRoute(page, '/integrations/api-keys')
    await page.getByRole('button', { name: /ci-accessibility/ }).click()
    const revokeDialog = page.getByRole('dialog', { name: zh.revokeKeyDialog })
    await expect(revokeDialog).toBeVisible()
    await revokeDialog.getByRole('button', { name: zh.revoke }).click()
    await expectToastStatus(page, apiKeyRevokeFailure)
    await expect(revokeDialog).toBeVisible()
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(
      diagnostics,
      ['/fb/v1/console/api-keys/key-a11y'],
      [conflictResourceConsole, apiKeyRevokeFailure],
    )
  })

  test('feedback retry-enrichment status messages are accessible', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/feedback/terminal-failures')
    await page.getByRole('button', { name: `${zh.retryEnrichment} #feedback-201` }).click()
    await expectToastStatus(page, zh.feedbackRetryQueued)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback retry-enrichment errors remain visible and axe-clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page, {
      fail: { feedbackRetryEnrichment: feedbackRetryFailure },
    })

    await gotoConsoleRoute(page, '/feedback/terminal-failures')
    await page.getByRole('button', { name: `${zh.retryEnrichment} #feedback-201` }).click()
    await expectToastStatus(page, zh.feedbackRetryFailed)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(
      diagnostics,
      ['/fb/v1/console/feedback/feedback-201'],
      [serverErrorResourceConsole],
    )
  })

  test('MCP tool policy controls are keyboard-addressable and axe-clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/mcp-clients')
    await page
      .getByRole('checkbox', {
        name: new RegExp(`${escapeRegExp(zh.denyTool)}\\s+list_feedback`),
      })
      .click()
    await page.getByRole('button', { name: zh.saveToolPolicies }).click()
    await expectToastStatus(page, zh.mcpToolsSaved)
    await expect(page.getByRole('table', { name: zh.mcpSessionsTable })).toBeVisible()
    await expect(page.getByRole('table', { name: zh.mcpGrantsTable })).toBeVisible()
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('MCP session and refresh grant revoke status messages are accessible', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/mcp-clients')
    await expect(page.getByRole('table', { name: zh.mcpSessionsTable })).toBeVisible()
    await page.getByRole('button', { name: zh.revokeSession }).click()
    await expectToastStatus(page, zh.mcpSessionRevoked)
    await page.getByRole('button', { name: zh.revokeGrant }).click()
    await expectToastStatus(page, zh.mcpGrantRevoked)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('MCP tool-policy errors remain visible and axe-clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page, {
      fail: { mcpToolPolicies: mcpToolPolicyFailure },
    })

    await gotoConsoleRoute(page, '/mcp-clients')
    await page
      .getByRole('checkbox', {
        name: new RegExp(`${escapeRegExp(zh.denyTool)}\\s+list_feedback`),
      })
      .click()
    await page.getByRole('button', { name: zh.saveToolPolicies }).click()
    await expectToastStatus(page, mcpToolPolicyFailure)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(
      diagnostics,
      ['/fb/v1/console/mcp/clients/client-a11y/tool-policies'],
      [conflictResourceConsole, mcpToolPolicyFailure],
    )
  })

  test('GDPR step-up dialog keeps focus and accessible labels intact', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/administration/gdpr')
    await page.getByRole('button', { name: zh.stepUp }).click()
    await expect(
      page.getByRole('dialog', { name: new RegExp(escapeRegExp(zh.stepUpDialog)) }),
    ).toBeVisible()
    await page.getByLabel(zh.currentPassword).fill('correct horse battery staple')
    await expectNoAxeViolations(page)
    await page.getByRole('button', { name: zh.stepUpConfirm }).click()
    await expect(
      page.getByRole('dialog', { name: new RegExp(escapeRegExp(zh.stepUpDialog)) }),
    ).toBeHidden()
    await expectToastStatus(page, zh.stepUpSuccess)
    await expectNoDocumentOverflow(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('GDPR validation errors are exposed without a network request', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/administration/gdpr')
    await page.getByRole('button', { name: zh.exportZip }).click()
    await expectToastStatus(page, zh.gdprSubjectRequired)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('dead delivery retry action remains accessible after mutation', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/administration/dead-deliveries')
    await page
      .getByRole('button', { name: new RegExp(`${escapeRegExp(zh.retryDelivery)}.*example`) })
      .click()
    await expectToastStatus(page, zh.outboxRetryQueued)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('dead delivery retry errors remain visible and axe-clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page, {
      fail: { outboxRetry: outboxRetryFailure },
    })

    await gotoConsoleRoute(page, '/administration/dead-deliveries')
    await page
      .getByRole('button', { name: new RegExp(`${escapeRegExp(zh.retryDelivery)}.*example`) })
      .click()
    await expectToastStatus(page, outboxRetryFailure)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(
      diagnostics,
      ['/fb/v1/console/outbox/delivery-a11y/retry'],
      [conflictResourceConsole],
    )
  })
})

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

async function expectRouteShellState(page: Page, routeCase: (typeof routes)[number]) {
  await expect(page).toHaveTitle(new RegExp(`${escapeRegExp(routeCase.title)}.*Attune Console`))
  await expect(page.getByRole('heading', { level: 1, name: routeCase.heading })).toBeVisible()
  await expect(page.locator('main#main-content')).toBeVisible()
  await expect(page.locator('a[href="#main-content"]')).toHaveCount(1)
  await expect(page.locator('[role="dialog"]:visible')).toHaveCount(0)
}

async function cycleSheet(page: Page, opener: Locator, cycles: number) {
  await expect(opener).toBeVisible()

  for (let cycle = 0; cycle < cycles; cycle += 1) {
    await opener.click()
    await expect(page.getByRole('dialog')).toBeVisible()
    if (cycle === 0) {
      await expectNoAxeViolations(page)
    }
    await expectNoDocumentOverflow(page)

    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expectDomFocus(opener)
    await expectNoDocumentOverflow(page)
  }
}

async function expectDomFocus(locator: Locator) {
  await expect
    .poll(
      async () =>
        locator.evaluate((element) => {
          const active = document.activeElement
          if (active === element) return 'target'
          const activeText = active instanceof HTMLElement ? active.innerText.slice(0, 120) : ''
          const targetText = element instanceof HTMLElement ? element.innerText.slice(0, 120) : ''
          return JSON.stringify({
            activeConnected: active instanceof HTMLElement ? active.isConnected : null,
            activeRole: active?.getAttribute('role') ?? null,
            activeTag: active?.tagName ?? null,
            activeText,
            documentHasFocus: document.hasFocus(),
            targetConnected: element.isConnected,
            targetText,
          })
        }),
      {
        message: 'expected the opener to be document.activeElement after close',
      },
    )
    .toBe('target')
}

async function expectToastStatus(page: Page, message: string) {
  const liveRegion = page.getByRole('region', { name: /^Notifications alt\+T$/ })
  await expect(liveRegion).toHaveAttribute('aria-live', 'polite')
  await expect(liveRegion).toHaveAttribute('aria-relevant', 'additions text')
  await expect(liveRegion.getByText(message)).toBeVisible()
}

async function expectAppShellVisibleToAxe(page: Page) {
  await expect(page.locator('.min-h-screen[aria-hidden="true"]')).toHaveCount(0)
}

async function clickShellNav(page: Parameters<typeof gotoConsoleRoute>[0], name: string) {
  const links = page.getByRole('link', { name: new RegExp(`^${escapeRegExp(name)}$`) })
  const visibleLinks = links.filter({ visible: true })

  if ((await visibleLinks.count()) === 0) {
    await page.getByRole('button', { name: zh.openNav }).click()
  }

  await expect(visibleLinks).toHaveCount(1)
  await visibleLinks.click()
}
