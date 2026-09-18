<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Bell, CircleCheck, DataAnalysis, Plus, Warning } from '@element-plus/icons-vue'
import { readingApi } from '@/api/readings'
import { pondApi } from '@/api/ponds'
import MetricCard from '@/components/common/MetricCard.vue'
import RiskTag from '@/components/common/RiskTag.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useAuth } from '@/hooks/useAuth'
import { useQueryParams } from '@/hooks/useQueryParams'
import type { Pond, WaterReading, WaterReadingInput } from '@/types/models'
import type { ReadingReviewStatus } from '@/types/enums'
import { readingReviewLabels } from '@/types/enums'
import { errorMessage } from '@/utils/errors'
import { formatDateTime, toISO, toLocalInput } from '@/utils/format'

const { canOperate, canReview, user } = useAuth()
const { params } = useQueryParams({ status: '', pondId: '', page: 1 })
const readings = ref<WaterReading[]>([])
const ponds = ref<Pond[]>([])
const total = ref(0)
const loading = ref(false)
const saving = ref(false)
const editorOpen = ref(false)
const confirmOpen = ref(false)
const reviewOpen = ref(false)
const deleteOpen = ref(false)
const target = ref<WaterReading | null>(null)
const confirmationNote = ref('')
const reviewApproved = ref(true)
const reviewNote = ref('')
const measuredAtLocal = ref(toLocalInput())
const form = reactive<WaterReadingInput>({ pondId: 0, dissolvedOxygen: 6, temperature: 26, ph: 7.5, ammonia: 0.1, turbidity: 25, measuredAt: '', source: 'manual' })

const warningCount = computed(() => readings.value.filter((item) => item.riskLevel === 'warning').length)
const criticalCount = computed(() => readings.value.filter((item) => item.riskLevel === 'critical').length)
// 所有未生效的异常读数（待确认预警、待核实/待复核/已否决严重）均不得放行。
const blockingCount = computed(() => readings.value.filter((item) => item.riskLevel !== 'normal' && !item.confirmed).length)
const pendingReviewCount = computed(() => readings.value.filter((item) => item.reviewStatus === 'pending_review').length)

async function load() {
  loading.value = true
  try {
    const [result, pondResult] = await Promise.all([
      readingApi.list({ page: Number(params.page), pageSize: 20, status: String(params.status), pondId: Number(params.pondId) || undefined }),
      pondApi.list({ page: 1, pageSize: 100 }),
    ])
    readings.value = result.items
    total.value = result.total
    ponds.value = pondResult.items
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  Object.assign(form, { pondId: ponds.value[0]?.id || 0, dissolvedOxygen: 6, temperature: 26, ph: 7.5, ammonia: 0.1, turbidity: 25, source: 'manual' })
  measuredAtLocal.value = toLocalInput()
  editorOpen.value = true
}

async function create() {
  if (!form.pondId) {
    ElMessage.warning('请选择养殖池')
    return
  }
  saving.value = true
  try {
    await readingApi.create({ ...form, measuredAt: toISO(measuredAtLocal.value) })
    ElMessage.success('水质读数已录入')
    editorOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

// 第一级：预警读数为单级确认；严重读数只生成待复核记录，不解除异常。
function openConfirm(reading: WaterReading) {
  target.value = reading
  confirmationNote.value = ''
  confirmOpen.value = true
}

const targetCritical = computed(() => target.value?.riskLevel === 'critical')

async function confirmReading() {
  if (!target.value || confirmationNote.value.trim().length < 2) {
    ElMessage.warning(targetCritical.value ? '请填写现场核实情况（至少 2 个字）' : '请填写处置或确认说明')
    return
  }
  saving.value = true
  try {
    await readingApi.confirm(target.value.id, confirmationNote.value)
    ElMessage.success(targetCritical.value ? '已生成待复核记录，等待另一名操作员终审' : '异常读数已确认')
    confirmOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

// 第二级：另一名操作员通过/否决待复核的严重读数。
function openReview(reading: WaterReading) {
  target.value = reading
  reviewApproved.value = true
  reviewNote.value = ''
  reviewOpen.value = true
}

async function submitReview() {
  if (!target.value) return
  if (!reviewApproved.value && reviewNote.value.trim().length < 2) {
    ElMessage.warning('否决严重读数必须写明原因')
    return
  }
  saving.value = true
  try {
    await readingApi.review(target.value.id, reviewApproved.value, reviewNote.value)
    ElMessage.success(reviewApproved.value ? '复核通过，严重读数已生效' : '已否决，读数保持严重并记录原因')
    reviewOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

async function remove() {
  if (!target.value) return
  saving.value = true
  try {
    await readingApi.remove(target.value.id)
    ElMessage.success('手工读数已删除')
    deleteOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

function reviewTagType(status: ReadingReviewStatus) {
  switch (status) {
    case 'approved':
      return 'success'
    case 'pending_review':
      return 'warning'
    case 'rejected':
      return 'danger'
    default:
      return 'info'
  }
}

function canVerify(row: WaterReading) {
  if (!canOperate() || row.confirmed) return false
  if (row.riskLevel === 'warning') return row.reviewStatus === 'unverified'
  if (row.riskLevel === 'critical') return row.reviewStatus === 'unverified' || row.reviewStatus === 'rejected'
  return false
}

// 第二级终审必须由首名核实人之外的操作员完成。
function canSecondReview(row: WaterReading) {
  return canOperate() &&
    row.riskLevel === 'critical' &&
    row.reviewStatus === 'pending_review' &&
    !row.confirmed &&
    row.verifiedByUserId !== user.value?.id
}

function deletable(row: WaterReading) {
  return canReview() && row.source === 'manual' && !row.confirmed &&
    row.reviewStatus !== 'pending_review' && row.reviewStatus !== 'rejected'
}

let timer: number | undefined
watch(params, () => { window.clearTimeout(timer); timer = window.setTimeout(load, 200) }, { deep: true })
onMounted(load)
</script>

<template>
  <div class="page-stack">
    <section class="metrics-grid">
      <MetricCard label="页内读数" :value="readings.length" :icon="DataAnalysis" hint="按测量时间倒序" />
      <MetricCard label="正常" :value="readings.length - warningCount - criticalCount" :icon="CircleCheck" tone="green" />
      <MetricCard label="预警 / 严重" :value="`${warningCount} / ${criticalCount}`" :icon="Warning" tone="amber" />
      <MetricCard label="待放行异常" :value="blockingCount" :icon="Bell" tone="red" :hint="pendingReviewCount ? `其中 ${pendingReviewCount} 条等待第二级复核` : '需人工复核'" />
    </section>
    <section class="workspace-panel">
      <div class="panel-toolbar">
        <div class="filters">
          <el-select v-model="params.pondId" placeholder="全部养殖池" clearable><el-option v-for="pond in ponds" :key="pond.id" :label="pond.name" :value="String(pond.id)" /></el-select>
          <el-select v-model="params.status" placeholder="全部风险" clearable><el-option label="正常" value="normal" /><el-option label="预警" value="warning" /><el-option label="严重" value="critical" /></el-select>
        </div>
        <el-button v-if="canOperate()" type="primary" :icon="Plus" @click="openCreate">录入读数</el-button>
      </div>
      <el-table v-loading="loading" :data="readings" stripe empty-text="暂无水质读数">
        <el-table-column label="养殖池 / 时间" min-width="190"><template #default="{ row }"><div class="primary-cell"><strong>{{ row.pond?.name || `#${row.pondId}` }}</strong><small>{{ formatDateTime(row.measuredAt) }}</small></div></template></el-table-column>
        <el-table-column label="溶解氧" width="105"><template #default="{ row }">{{ row.dissolvedOxygen }} mg/L</template></el-table-column>
        <el-table-column label="水温" width="85"><template #default="{ row }">{{ row.temperature }}℃</template></el-table-column>
        <el-table-column label="pH" prop="ph" width="70" />
        <el-table-column label="氨氮" width="85"><template #default="{ row }">{{ row.ammonia }}</template></el-table-column>
        <el-table-column label="风险" width="90"><template #default="{ row }"><RiskTag :level="row.riskLevel" /></template></el-table-column>
        <el-table-column label="判定说明" prop="alertMessage" min-width="220" show-overflow-tooltip />
        <el-table-column label="复核闭环" min-width="150">
          <template #default="{ row }">
            <div v-if="row.riskLevel === 'normal'" class="muted">—</div>
            <div v-else class="review-cell">
              <el-tag :type="reviewTagType(row.reviewStatus)" size="small" effect="plain">{{ readingReviewLabels[row.reviewStatus as ReadingReviewStatus] }}</el-tag>
              <small v-if="row.reviewStatus === 'pending_review'" class="muted">首核：{{ row.verifiedBy || '—' }}</small>
              <small v-else-if="row.confirmed" class="confirmed-text">{{ row.confirmedBy }} 已放行</small>
              <small v-else-if="row.reviewStatus === 'rejected'" class="reject-text">否决：{{ row.reviewBy || '—' }}</small>
            </div>
          </template>
        </el-table-column>
        <el-table-column v-if="canOperate()" label="操作" width="210" fixed="right">
          <template #default="{ row }">
            <el-button v-if="canVerify(row)" link type="primary" @click="openConfirm(row)">{{ row.riskLevel === 'critical' ? '核实送审' : '确认' }}</el-button>
            <el-button v-if="canSecondReview(row)" link type="warning" @click="openReview(row)">第二级复核</el-button>
            <el-button v-if="deletable(row)" link type="danger" @click="target = row; deleteOpen = true">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pagination"><el-pagination v-model:current-page="params.page" layout="total, prev, pager, next" :total="total" :page-size="20" /></div>
    </section>
    <el-dialog v-model="editorOpen" title="录入水质读数" width="680px">
      <el-alert title="保存后系统将自动评估风险等级；严重读数需两名操作员先后复核" type="info" :closable="false" show-icon />
      <el-form label-position="top" class="form-grid form-with-alert">
        <el-form-item label="养殖池"><el-select v-model="form.pondId"><el-option v-for="pond in ponds" :key="pond.id" :label="`${pond.name} (${pond.code})`" :value="pond.id" /></el-select></el-form-item>
        <el-form-item label="测量时间"><el-date-picker v-model="measuredAtLocal" type="datetime" value-format="YYYY-MM-DDTHH:mm" /></el-form-item>
        <el-form-item label="溶解氧（mg/L）"><el-input-number v-model="form.dissolvedOxygen" :min="0" :max="30" :step="0.1" /></el-form-item>
        <el-form-item label="水温（℃）"><el-input-number v-model="form.temperature" :min="-5" :max="50" :step="0.1" /></el-form-item>
        <el-form-item label="pH"><el-input-number v-model="form.ph" :min="0" :max="14" :step="0.1" /></el-form-item>
        <el-form-item label="氨氮（mg/L）"><el-input-number v-model="form.ammonia" :min="0" :max="20" :step="0.01" /></el-form-item>
        <el-form-item label="浊度（NTU）"><el-input-number v-model="form.turbidity" :min="0" :max="1000" :step="1" /></el-form-item>
        <el-form-item label="来源"><el-select v-model="form.source"><el-option label="手工录入" value="manual" /><el-option label="传感器" value="sensor" /><el-option label="数据导入" value="import" /></el-select></el-form-item>
      </el-form>
      <template #footer><el-button @click="editorOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="create">保存并评估</el-button></template>
    </el-dialog>
    <el-dialog v-model="confirmOpen" :title="targetCritical ? '严重读数现场核实（第一级）' : '确认水质异常'" width="520px">
      <div v-if="target" class="risk-summary">
        <RiskTag :level="target.riskLevel" />
        <p>{{ target.alertMessage }}</p>
        <el-alert
          v-if="targetCritical"
          title="核实后仅生成待复核记录，读数仍保持严重：不能解除异常、不能据此批准计划或安排投喂，须由另一名操作员第二级复核通过后才生效。"
          type="warning" :closable="false" show-icon
        />
        <el-alert v-else title="预警读数为单级确认，确认后即生效。" type="info" :closable="false" show-icon />
      </div>
      <el-form-item :label="targetCritical ? '现场核实情况' : '处置 / 复核说明'"><el-input v-model="confirmationNote" type="textarea" :rows="4" placeholder="记录现场复核情况和已采取的措施" /></el-form-item>
      <template #footer><el-button @click="confirmOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="confirmReading">{{ targetCritical ? '核实并提交复核' : '确认并留痕' }}</el-button></template>
    </el-dialog>
    <el-dialog v-model="reviewOpen" title="严重读数第二级复核" width="520px">
      <div v-if="target" class="risk-summary">
        <RiskTag :level="target.riskLevel" />
        <p>{{ target.alertMessage }}</p>
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="首名核实人">{{ target.verifiedBy || '—' }}</el-descriptions-item>
          <el-descriptions-item label="核实时间">{{ target.verifiedAt ? formatDateTime(target.verifiedAt) : '—' }}</el-descriptions-item>
          <el-descriptions-item label="现场核实情况">{{ target.confirmationNote || '—' }}</el-descriptions-item>
        </el-descriptions>
        <el-alert title="通过后读数才生效；否决则读数保持严重并继续阻断计划批准与投喂安排。" type="warning" :closable="false" show-icon />
      </div>
      <el-form label-position="top" class="review-form">
        <el-form-item label="复核结论">
          <el-radio-group v-model="reviewApproved">
            <el-radio :value="true">复核通过，读数生效</el-radio>
            <el-radio :value="false">否决，维持严重</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="reviewApproved ? '复核备注（可选）' : '否决原因（必填）'">
          <el-input v-model="reviewNote" type="textarea" :rows="3" :placeholder="reviewApproved ? '可补充复核情况' : '请写明否决原因'" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="reviewOpen = false">取消</el-button><el-button :type="reviewApproved ? 'success' : 'danger'" :loading="saving" @click="submitReview">提交复核结论</el-button></template>
    </el-dialog>
    <ConfirmDialog v-model="deleteOpen" title="删除手工读数" message="删除后仍会保留操作审计，确认继续？" danger :loading="saving" @confirm="remove" />
  </div>
</template>

<style scoped>
.review-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
  align-items: flex-start;
}
.reject-text {
  color: var(--el-color-danger);
}
.review-form {
  margin-top: 12px;
}
</style>
