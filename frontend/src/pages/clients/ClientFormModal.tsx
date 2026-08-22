import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  AutoComplete,
  Button,
  Card,
  Col,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Switch,
  Spin,
  Tabs,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd';
import {
  DeleteOutlined,
  EyeOutlined,
  PlusOutlined,
  ReloadOutlined,
  RetweetOutlined,
} from '@ant-design/icons';
import dayjs from 'dayjs';
import type { Dayjs } from 'dayjs';
import { Controller, FormProvider, useForm, useWatch, useFieldArray } from 'react-hook-form';

import { HttpUtil, IntlUtil, RandomUtil, Wireguard } from '@/utils';
import { formatInboundLabel } from '@/lib/inbounds/label';
import { generateMtprotoSecret } from '@/lib/xray/inbound-defaults';
import { normalizeClientIps, type ClientIpInfo } from '@/lib/clients/ip-log';
import { useDatepicker } from '@/hooks/useDatepicker';
import { useClientHwids } from '@/hooks/useClientHwids';
import { DateTimePicker, SelectAllClearButtons } from '@/components/form';
import { FormField } from '@/components/form/rhf';
import ClientHwidListModal from '@/components/clients/ClientHwidList';
import { TLS_FLOW_CONTROL, TRAFFIC_RESETS } from '@/schemas/primitives';
import type {
  ClientRecord,
  InboundOption,
  ExternalLink,
  ExternalLinkInput,
  ClientNodeQuotaInput,
  ClientNodeQuotaView,
} from '@/hooks/useClients';
import { useFail2banStatusQuery, getLimitIpNotice } from '@/api/queries/useFail2banStatusQuery';
import { ClientFormSchema, ClientCreateFormSchema, type ClientFormValues } from '@/schemas/client';
import type { NodeRecord } from '@/schemas/node';
import {
  buildNodeQuotaRows,
  bytesToQuotaGB,
  quotaGBToBytes,
  serializeNodeQuotaRows,
  type ClientNodeQuotaRow,
} from './clientNodeQuotaForm';

const FLOW_OPTIONS = Object.values(TLS_FLOW_CONTROL);
const VMESS_SECURITY_OPTIONS = ['auto', 'aes-128-gcm', 'chacha20-poly1305'] as const;

const MULTI_CLIENT_PROTOCOLS = new Set([
  'shadowsocks',
  'vless',
  'vmess',
  'trojan',
  'hysteria',
  'wireguard',
  'mtproto',
]);

const CLIENT_FORM_MODAL_Z_INDEX = 1000;
const CLIENT_IP_LOG_MODAL_Z_INDEX = CLIENT_FORM_MODAL_Z_INDEX + 1;
const EMPTY_NODE_RECORDS: NodeRecord[] = [];

interface ExternalLinkRow {
  kind: 'link' | 'subscription';
  value: string;
  remark: string;
  enable: boolean;
  expiryTime: number;
  namePrefix: string;
  lastFetchAt: number;
  lastFetchError: string;
}

interface ApiMsg<T = unknown> {
  success?: boolean;
  msg?: string;
  obj?: T | null;
}

type NodeQuotaResponse = {
  nodeQuotas: ClientNodeQuotaView[];
  pendingNodeIds?: number[];
  nodeId?: number;
  pending?: boolean;
};

type NodeQuotaRead = (email: string) => Promise<ClientNodeQuotaView[]>;
type NodeQuotaReset = (email: string, nodeId: number) => Promise<ApiMsg<NodeQuotaResponse> | null>;
type NodeQuotaResetAll = (email: string) => Promise<ApiMsg<NodeQuotaResponse> | null>;

type Mode = 'add' | 'edit';

interface SaveMetaEdit {
  isEdit: true;
  email: string;
  attach: number[];
  detach: number[];
  externalLinks: ExternalLinkInput[];
  nodeQuotas: ClientNodeQuotaInput[];
  originalNodeQuotas: ClientNodeQuotaInput[];
}

interface SaveMetaCreate {
  isEdit: false;
  email: string;
  externalLinks: ExternalLinkInput[];
  nodeQuotas: ClientNodeQuotaInput[];
}

interface SaveCreatePayload {
  client: Record<string, unknown>;
  inboundIds: number[];
}

interface ClientFormModalProps {
  open: boolean;
  mode: Mode;
  client: ClientRecord | null;
  inbounds: InboundOption[];
  attachedExternalLinks?: ExternalLink[];
  attachedIds?: number[];
  tgBotEnable?: boolean;
  groups?: string[];
  nodes?: NodeRecord[];
  getNodeQuotas?: NodeQuotaRead;
  resetNodeQuota?: NodeQuotaReset;
  resetAllNodeQuotas?: NodeQuotaResetAll;
  save: (
    payload: Record<string, unknown> | SaveCreatePayload,
    meta: SaveMetaEdit | SaveMetaCreate,
  ) => Promise<ApiMsg | null>;
  resetTraffic?: (client: ClientRecord) => Promise<ApiMsg | null>;
  onOpenChange: (open: boolean) => void;
}

type Values = ClientFormValues & {
  expiryDate: number;
  limitHwid: number;
  externalLinks: ExternalLinkRow[];
  wgPrivateKey: string;
  wgPublicKey: string;
  wgPreSharedKey: string;
  wgAllowedIPs: string;
  secret: string;
  adTag: string;
};

const EMPTY: Values = {
  email: '',
  subId: '',
  uuid: '',
  password: '',
  auth: '',
  flow: '',
  security: 'auto',
  reverseTag: '',
  totalGB: 0,
  expiryDate: 0,
  delayedStart: false,
  delayedDays: 0,
  reset: 0,
  resetDay: 0,
  resetMax: 0,
  trafficReset: 'never' as const,
  trafficResetDay: 1,
  limitIp: 0,
  limitHwid: 0,
  tgId: 0,
  group: '',
  comment: '',
  enable: true,
  inboundIds: [],
  externalLinks: [],
  wgPrivateKey: '',
  wgPublicKey: '',
  wgPreSharedKey: '',
  wgAllowedIPs: '',
  secret: '',
  adTag: '',
};

function toExternalLinkRows(links: ExternalLink[] | undefined): ExternalLinkRow[] {
  return (links || []).map((l) => ({
    kind: l.kind === 'subscription' ? 'subscription' : 'link',
    value: l.value || '',
    remark: l.remark || '',
    enable: l.enable !== false,
    expiryTime: Number(l.expiryTime) || 0,
    namePrefix: l.namePrefix || '',
    lastFetchAt: Number(l.lastFetchAt) || 0,
    lastFetchError: l.lastFetchError || '',
  }));
}

function bytesToGB(bytes: number): number {
  if (!bytes || bytes <= 0) return 0;
  return Math.round((bytes / (1024 * 1024 * 1024)) * 100) / 100;
}

export function gbToBytes(gb: number): number {
  if (!gb || gb <= 0) return 0;
  return Math.round(gb * 1024 * 1024 * 1024);
}

export function resolveTotalBytes(
  originalBytes: number | null | undefined,
  displayedGB: number,
): number {
  if (originalBytes != null && displayedGB === bytesToGB(originalBytes)) {
    return originalBytes;
  }
  return gbToBytes(displayedGB);
}

export default function ClientFormModal({
  open,
  mode,
  client,
  inbounds,
  attachedExternalLinks = [],
  attachedIds = [],
  tgBotEnable = false,
  groups = [],
  nodes = EMPTY_NODE_RECORDS,
  getNodeQuotas,
  resetNodeQuota,
  resetAllNodeQuotas,
  save,
  resetTraffic,
  onOpenChange,
}: ClientFormModalProps) {
  const { t } = useTranslation();
  const [messageApi, messageContextHolder] = message.useMessage();
  const isEdit = mode === 'edit';

  const methods = useForm<Values>({ defaultValues: EMPTY });
  const inboundIds = useWatch({ control: methods.control, name: 'inboundIds' });
  const delayedStart = useWatch({ control: methods.control, name: 'delayedStart' });
  const expiryDate = useWatch({ control: methods.control, name: 'expiryDate' });
  const enable = useWatch({ control: methods.control, name: 'enable' });
  const flow = useWatch({ control: methods.control, name: 'flow' });
  const reverseTag = useWatch({ control: methods.control, name: 'reverseTag' });
  const secret = useWatch({ control: methods.control, name: 'secret' });
  const email = useWatch({ control: methods.control, name: 'email' });
  const uuid = useWatch({ control: methods.control, name: 'uuid' });
  const trafficReset = useWatch({ control: methods.control, name: 'trafficReset' });
  const password = useWatch({ control: methods.control, name: 'password' });
  const subId = useWatch({ control: methods.control, name: 'subId' });
  const limitHwid = useWatch({ control: methods.control, name: 'limitHwid' });
  const auth = useWatch({ control: methods.control, name: 'auth' });
  const wgPrivateKey = useWatch({ control: methods.control, name: 'wgPrivateKey' });
  const limitIp = useWatch({ control: methods.control, name: 'limitIp' });
  const {
    fields: externalLinkFields,
    append: appendExternalLink,
    remove: removeExternalLink,
  } = useFieldArray({ control: methods.control, name: 'externalLinks' });

  const [submitting, setSubmitting] = useState(false);
  const [resetting, setResetting] = useState(false);
  const [nodeQuotaRows, setNodeQuotaRows] = useState<ClientNodeQuotaRow[]>([]);
  const [nodeQuotaViews, setNodeQuotaViews] = useState<ClientNodeQuotaView[]>([]);
  const [nodeQuotaLoading, setNodeQuotaLoading] = useState(false);
  const [nodeQuotaError, setNodeQuotaError] = useState('');
  const [nodeQuotaBusy, setNodeQuotaBusy] = useState<number | 'all' | null>(null);
  const nodeQuotaDirty = useRef(false);
  const [clientIps, setClientIps] = useState<ClientIpInfo[]>([]);
  const [ipsLoading, setIpsLoading] = useState(false);
  const [ipsClearing, setIpsClearing] = useState(false);
  const [ipsModalOpen, setIpsModalOpen] = useState(false);
  const {
    clientHwids,
    hwidsLoading,
    hwidsClearing,
    deletingHwidId,
    loadHwids,
    clearHwids,
    deleteHwid,
  } = useClientHwids(client?.email);
  const [hwidsModalOpen, setHwidsModalOpen] = useState(false);
  const { datepicker } = useDatepicker();
  const hwidDateLabel = (ts: number) =>
    !ts || ts <= 0 ? '-' : IntlUtil.formatDate(ts, datepicker);
  const fail2ban = useFail2banStatusQuery();
  const limitIpDisabled = !fail2ban.usable;
  const limitIpNotice = getLimitIpNotice(fail2ban, t);

  function addExternalLinkRow(kind: 'link' | 'subscription') {
    appendExternalLink({
      kind,
      value: '',
      remark: '',
      enable: true,
      expiryTime: 0,
      namePrefix: '',
      lastFetchAt: 0,
      lastFetchError: '',
    });
  }

  useEffect(() => {
    if (!open) return;
    setIpsModalOpen(false);
    setHwidsModalOpen(false);

    if (isEdit && client) {
      const et = Number(client.expiryTime) || 0;
      const seed: Values = {
        ...EMPTY,
        email: client.email || '',
        subId: client.subId || '',
        uuid: client.uuid || '',
        password: client.password || '',
        auth: client.auth || '',
        flow: client.flow || '',
        security:
          !client.security || client.security === 'none' || client.security === 'zero'
            ? 'auto'
            : client.security,
        reverseTag: client.reverse?.tag || '',
        totalGB: bytesToGB(client.totalGB || 0),
        reset: Number(client.reset) || 0,
        resetDay: Number(client.resetDay) || 0,
        resetMax: Number(client.resetMax) || 0,
        trafficReset: (client.trafficReset as ClientFormValues['trafficReset']) || 'never',
        trafficResetDay: Number(client.trafficResetDay) || 1,
        limitIp: client.limitIp || 0,
        limitHwid: client.limitHwid || 0,
        tgId: Number(client.tgId) || 0,
        group: client.group || '',
        comment: client.comment || '',
        enable: !!client.enable,
        inboundIds: Array.isArray(attachedIds) ? [...attachedIds] : [],
        externalLinks: toExternalLinkRows(attachedExternalLinks),
        wgPrivateKey: client.privateKey || '',
        wgPublicKey: client.publicKey || '',
        wgPreSharedKey: client.preSharedKey || '',
        wgAllowedIPs: client.allowedIPs || '',
        secret: client.secret || '',
        adTag: client.adTag || '',
      };
      if (et < 0) {
        seed.delayedStart = true;
        seed.delayedDays = Math.round(et / -86400000);
        seed.expiryDate = 0;
      } else {
        seed.delayedStart = false;
        seed.delayedDays = 0;
        seed.expiryDate = et > 0 ? et : 0;
      }
      methods.reset(seed);
      void loadIps();
      void loadHwids();
    } else {
      const wgKeypair = Wireguard.generateKeypair();
      methods.reset({
        ...EMPTY,
        email: RandomUtil.randomLowerAndNum(10),
        uuid: RandomUtil.randomUUID(),
        subId: RandomUtil.randomLowerAndNum(16),
        password: RandomUtil.randomLowerAndNum(16),
        auth: RandomUtil.randomLowerAndNum(16),
        wgPrivateKey: wgKeypair.privateKey,
        wgPublicKey: wgKeypair.publicKey,
      });
    }

    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, isEdit]);

  useEffect(() => {
    if (!open) return;
    nodeQuotaDirty.current = false;
    setNodeQuotaError('');
    if (!isEdit || !client?.email || !getNodeQuotas) {
      setNodeQuotaViews([]);
      setNodeQuotaLoading(false);
      return;
    }
    let active = true;
    setNodeQuotaLoading(true);
    void getNodeQuotas(client.email)
      .then((views) => {
        if (!active) return;
        setNodeQuotaViews(views);
        setNodeQuotaError('');
      })
      .catch((err: unknown) => {
        if (!active) return;
        setNodeQuotaError(err instanceof Error ? err.message : 'Failed to load node quotas');
      })
      .finally(() => {
        if (active) setNodeQuotaLoading(false);
      });
    return () => {
      active = false;
    };
  }, [open, isEdit, client?.email, getNodeQuotas]);

  useEffect(() => {
    if (!open || nodeQuotaDirty.current) return;
    setNodeQuotaRows(buildNodeQuotaRows(nodes, nodeQuotaViews));
  }, [open, nodes, nodeQuotaViews]);

  const flowCapableIds = useMemo(() => {
    const ids = new Set<number>();
    for (const row of inbounds || []) {
      if (row?.tlsFlowCapable) ids.add(row.id);
    }
    return ids;
  }, [inbounds]);

  const vlessLikeIds = useMemo(() => {
    const ids = new Set<number>();
    for (const row of inbounds || []) {
      if (row && row.protocol === 'vless') ids.add(row.id);
    }
    return ids;
  }, [inbounds]);

  const vmessIds = useMemo(() => {
    const ids = new Set<number>();
    for (const row of inbounds || []) {
      if (row && row.protocol === 'vmess') ids.add(row.id);
    }
    return ids;
  }, [inbounds]);

  const wireguardIds = useMemo(() => {
    const ids = new Set<number>();
    for (const row of inbounds || []) {
      if (row && row.protocol === 'wireguard') ids.add(row.id);
    }
    return ids;
  }, [inbounds]);

  const mtprotoIds = useMemo(() => {
    const ids = new Set<number>();
    for (const row of inbounds || []) {
      if (row && row.protocol === 'mtproto') ids.add(row.id);
    }
    return ids;
  }, [inbounds]);

  const mtprotoDomain = useMemo(() => {
    for (const id of inboundIds || []) {
      const ib = (inbounds || []).find((row) => row.id === id);
      if (ib?.protocol === 'mtproto' && ib.mtprotoDomain) return ib.mtprotoDomain;
    }
    return 'www.cloudflare.com';
  }, [inboundIds, inbounds]);

  const ss2022Method = useMemo(() => {
    for (const id of inboundIds || []) {
      const ib = (inbounds || []).find((row) => row.id === id);
      const method = ib?.ssMethod;
      if (method && method.substring(0, 4) === '2022') return method;
    }
    return '';
  }, [inboundIds, inbounds]);

  function regeneratePassword() {
    methods.setValue(
      'password',
      ss2022Method
        ? RandomUtil.randomShadowsocksPassword(ss2022Method)
        : RandomUtil.randomLowerAndNum(16),
    );
  }

  const showFlow = useMemo(
    () => (inboundIds || []).some((id) => flowCapableIds.has(id)),
    [inboundIds, flowCapableIds],
  );

  const showReverseTag = useMemo(
    () => (inboundIds || []).some((id) => vlessLikeIds.has(id)),
    [inboundIds, vlessLikeIds],
  );

  const showSecurity = useMemo(
    () => (inboundIds || []).some((id) => vmessIds.has(id)),
    [inboundIds, vmessIds],
  );

  const showWireguard = useMemo(
    () => (inboundIds || []).some((id) => wireguardIds.has(id)),
    [inboundIds, wireguardIds],
  );

  const showMtproto = useMemo(
    () => (inboundIds || []).some((id) => mtprotoIds.has(id)),
    [inboundIds, mtprotoIds],
  );

  function regenerateWireguardKeys() {
    const kp = Wireguard.generateKeypair();
    methods.setValue('wgPrivateKey', kp.privateKey);
    methods.setValue('wgPublicKey', kp.publicKey);
  }

  function regenerateMtprotoSecret() {
    methods.setValue('secret', generateMtprotoSecret(mtprotoDomain));
  }

  useEffect(() => {
    // Only clear the flow once we actually have inbound options to judge
    // capability from. While the options list is momentarily empty (e.g. the
    // options query is (re)loading and `inbounds` falls back to `[]`), showFlow
    // is a false negative, so clearing here would silently drop a valid
    // xtls-rprx-vision flow the user picked for a Reality/TLS inbound.
    if (inbounds.length > 0 && !showFlow && flow) {
      methods.setValue('flow', '');
    }
  }, [inbounds, showFlow, flow, methods]);

  useEffect(() => {
    if (!showReverseTag && reverseTag) {
      methods.setValue('reverseTag', '');
    }
  }, [showReverseTag, reverseTag, methods]);

  useEffect(() => {
    if (!ss2022Method) return;
    const current = methods.getValues('password');
    if (!RandomUtil.isShadowsocks2022Password(current, ss2022Method)) {
      methods.setValue('password', RandomUtil.randomShadowsocksPassword(ss2022Method));
    }
  }, [ss2022Method, methods]);

  useEffect(() => {
    if (showMtproto && !secret) {
      methods.setValue('secret', generateMtprotoSecret(mtprotoDomain));
    }
  }, [showMtproto, secret, mtprotoDomain, methods]);

  const inboundOptions = useMemo(
    () =>
      (inbounds || [])
        .filter((ib) => MULTI_CLIENT_PROTOCOLS.has(ib.protocol || ''))
        .filter((ib) => ib.enable || (inboundIds || []).includes(ib.id))
        .map((ib) => ({
          label: formatInboundLabel(ib.tag, ib.remark),
          value: ib.id,
          title: formatInboundLabel(ib.tag, ib.remark),
        })),
    [inbounds, inboundIds],
  );

  const expiryDayjs = useMemo<Dayjs | null>(
    () => (expiryDate > 0 ? dayjs(expiryDate) : null),
    [expiryDate],
  );

  const linkRows = externalLinkFields
    .map((field, index) => ({ field, index }))
    .filter((row) => row.field.kind === 'link');
  const subscriptionRows = externalLinkFields
    .map((field, index) => ({ field, index }))
    .filter((row) => row.field.kind === 'subscription');

  async function loadIps() {
    if (!isEdit || !client?.email) return;
    setIpsLoading(true);
    try {
      const msg = (await HttpUtil.post(
        `/panel/api/clients/ips/${encodeURIComponent(client.email)}`,
      )) as ApiMsg<unknown[]>;
      if (!msg?.success) {
        setClientIps([]);
        return;
      }
      setClientIps(normalizeClientIps(msg.obj));
    } finally {
      setIpsLoading(false);
    }
  }

  function openIpsModal() {
    setIpsModalOpen(true);
    if (clientIps.length === 0) void loadIps();
  }

  async function clearIps() {
    if (!isEdit || !client?.email) return;
    setIpsClearing(true);
    try {
      const msg = (await HttpUtil.post(
        `/panel/api/clients/clearIps/${encodeURIComponent(client.email)}`,
      )) as ApiMsg;
      if (msg?.success) setClientIps([]);
    } finally {
      setIpsClearing(false);
    }
  }

  function openHwidsModal() {
    setHwidsModalOpen(true);
    if (clientHwids.length === 0) void loadHwids();
  }

  function close() {
    onOpenChange(false);
  }

  async function onResetTraffic() {
    if (!isEdit || !client?.email || !resetTraffic) return;
    setResetting(true);
    try {
      const msg = await resetTraffic(client);
      if (msg?.success) {
        messageApi.success(t('pages.clients.toasts.trafficReset'));
      } else {
        messageApi.error(msg?.msg || t('somethingWentWrong'));
      }
    } finally {
      setResetting(false);
    }
  }

  function updateNodeQuotaRow(nodeId: number, patch: Partial<ClientNodeQuotaRow>) {
    nodeQuotaDirty.current = true;
    setNodeQuotaRows((rows) =>
      rows.map((row) => (row.nodeId === nodeId ? { ...row, ...patch } : row)),
    );
  }

  function applyNodeQuotaResponse(msg: ApiMsg<NodeQuotaResponse> | null) {
    if (msg?.success && msg.obj) {
      nodeQuotaDirty.current = false;
      setNodeQuotaViews(msg.obj.nodeQuotas || []);
      setNodeQuotaRows(buildNodeQuotaRows(nodes, msg.obj.nodeQuotas || []));
    }
    return !!msg?.success;
  }

  async function onResetNodeQuota(nodeId: number) {
    if (!isEdit || !client?.email || !resetNodeQuota) return;
    setNodeQuotaBusy(nodeId);
    try {
      const msg = await resetNodeQuota(client.email, nodeId);
      if (!applyNodeQuotaResponse(msg)) {
        messageApi.error(msg?.msg || t('somethingWentWrong'));
      }
    } finally {
      setNodeQuotaBusy(null);
    }
  }

  async function onResetAllNodeQuotas() {
    if (!isEdit || !client?.email || !resetAllNodeQuotas) return;
    setNodeQuotaBusy('all');
    try {
      const msg = await resetAllNodeQuotas(client.email);
      if (!applyNodeQuotaResponse(msg)) {
        messageApi.error(msg?.msg || t('somethingWentWrong'));
      }
    } finally {
      setNodeQuotaBusy(null);
    }
  }

  async function onSubmit() {
    const values = methods.getValues();
    const schema = isEdit ? ClientFormSchema : ClientCreateFormSchema;
    const validated = schema.safeParse({
      email: values.email,
      subId: values.subId,
      uuid: values.uuid,
      password: values.password,
      auth: values.auth,
      flow: values.flow,
      security: values.security,
      reverseTag: values.reverseTag,
      totalGB: values.totalGB,
      delayedStart: values.delayedStart,
      delayedDays: values.delayedDays,
      reset: values.reset,
      resetDay: values.resetDay,
      resetMax: values.resetMax,
      trafficReset: values.trafficReset,
      trafficResetDay: values.trafficResetDay,
      limitIp: values.limitIp,
      limitHwid: values.limitHwid,
      tgId: values.tgId,
      group: values.group,
      comment: values.comment,
      enable: values.enable,
      inboundIds: values.inboundIds,
    });
    if (!validated.success) {
      const issue = validated.error.issues[0];
      messageApi.error(t(issue?.message ?? 'somethingWentWrong'));
      return;
    }
    const expiryTime = values.delayedStart
      ? -86400000 * (Number(values.delayedDays) || 0)
      : values.expiryDate || 0;
    const totalBytes = resolveTotalBytes(client ? (client.totalGB ?? 0) : null, values.totalGB);
    const clientPayload: Record<string, unknown> = {
      email: values.email.trim(),
      subId: values.subId,
      id: values.uuid,
      password: values.password,
      auth: values.auth,
      flow: showFlow ? values.flow || '' : '',
      security: showSecurity ? values.security || 'auto' : 'auto',
      totalGB: totalBytes,
      expiryTime,
      reset: Number(values.reset) || 0,
      resetDay: Number(values.resetDay) || 0,
      resetMax: Number(values.resetMax) || 0,
      trafficReset: values.trafficReset || 'never',
      trafficResetDay: Number(values.trafficResetDay) || 1,
      limitIp: Number(values.limitIp) || 0,
      limitHwid: Number(values.limitHwid) || 0,
      tgId: Number(values.tgId) || 0,
      group: values.group,
      comment: values.comment,
      enable: !!values.enable,
    };
    const reverseTagValue = showReverseTag ? (values.reverseTag || '').trim() : '';
    if (reverseTagValue) {
      clientPayload.reverse = { tag: reverseTagValue };
    }

    if (showWireguard) {
      clientPayload.privateKey = values.wgPrivateKey;
      clientPayload.publicKey = values.wgPublicKey;
      if (values.wgPreSharedKey) {
        clientPayload.preSharedKey = values.wgPreSharedKey;
      }
      const allowedIPs = values.wgAllowedIPs
        .split(',')
        .map((s) => s.trim())
        .filter((s) => s !== '');
      if (allowedIPs.length > 0) {
        clientPayload.allowedIPs = allowedIPs;
      }
    }

    if (showMtproto) {
      const adTag = values.adTag.trim();
      if (adTag !== '' && !/^[0-9a-fA-F]{32}$/.test(adTag)) {
        messageApi.error(t('pages.inbounds.form.mtgAdTagInvalid'));
        return;
      }
      clientPayload.secret = values.secret;
      clientPayload.adTag = adTag;
    }

    const externalLinks: ExternalLinkInput[] = values.externalLinks
      .map((r) => ({
        kind: r.kind,
        value: r.value.trim(),
        remark: (r.remark || '').trim(),
        enable: r.enable !== false,
        expiryTime: Number(r.expiryTime) || 0,
        namePrefix: (r.namePrefix || '').trim(),
      }))
      .filter((r) => r.value !== '');

    const nodeQuotas = serializeNodeQuotaRows(nodeQuotaRows);
    const originalNodeQuotas = serializeNodeQuotaRows(buildNodeQuotaRows([], nodeQuotaViews));

    setSubmitting(true);
    try {
      let msg;
      if (isEdit && client) {
        const original = new Set(attachedIds || []);
        const next = new Set(values.inboundIds || []);
        const toAttach = [...next].filter((id) => !original.has(id));
        const toDetach = [...original].filter((id) => !next.has(id));
        msg = await save(clientPayload, {
          isEdit: true,
          email: client.email,
          attach: toAttach,
          detach: toDetach,
          externalLinks,
          nodeQuotas,
          originalNodeQuotas,
        });
      } else {
        msg = await save(
          { client: clientPayload, inboundIds: values.inboundIds },
          { isEdit: false, email: clientPayload.email as string, externalLinks, nodeQuotas },
        );
      }
      if (msg?.success) close();
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <>
      {messageContextHolder}
      <Modal
        open={open}
        title={isEdit ? t('pages.clients.editClient') : t('pages.clients.addClient')}
        destroyOnHidden
        width={720}
        zIndex={CLIENT_FORM_MODAL_Z_INDEX}
        style={{ top: 20 }}
        styles={{
          body: { maxHeight: 'calc(100vh - 160px)', overflowY: 'auto', overflowX: 'hidden' },
        }}
        onCancel={close}
        footer={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            {isEdit && resetTraffic && (
              <Popconfirm
                title={t('pages.inbounds.resetTraffic')}
                description={t('pages.inbounds.resetTrafficContent')}
                okText={t('reset')}
                cancelText={t('cancel')}
                zIndex={CLIENT_IP_LOG_MODAL_Z_INDEX}
                onConfirm={onResetTraffic}
              >
                <Button
                  color="danger"
                  variant="filled"
                  icon={<RetweetOutlined />}
                  loading={resetting}
                >
                  {t('pages.inbounds.resetTraffic')}
                </Button>
              </Popconfirm>
            )}
            <div style={{ marginInlineStart: 'auto', display: 'flex', gap: 8 }}>
              <Button onClick={close}>{t('cancel')}</Button>
              <Button
                type="primary"
                loading={submitting}
                disabled={nodeQuotaLoading || !!nodeQuotaError}
                onClick={onSubmit}
              >
                {isEdit ? t('save') : t('create')}
              </Button>
            </div>
          </div>
        }
      >
        <FormProvider {...methods}>
          <Form layout="vertical">
            <Tabs
              defaultActiveKey="basic"
              items={[
                {
                  key: 'basic',
                  label: t('pages.clients.tabBasics'),
                  children: (
                    <>
                      <Row gutter={16}>
                        <Col xs={24} md={12}>
                          <Form.Item label={t('pages.clients.email')} required>
                            <Space.Compact style={{ display: 'flex' }}>
                              <Input
                                value={email}
                                placeholder={t('pages.clients.email')}
                                style={{ flex: 1 }}
                                onChange={(e) => methods.setValue('email', e.target.value)}
                              />
                              {!isEdit && (
                                <Button
                                  aria-label={t('regenerate')}
                                  icon={<ReloadOutlined />}
                                  onClick={() =>
                                    methods.setValue('email', RandomUtil.randomLowerAndNum(12))
                                  }
                                />
                              )}
                            </Space.Compact>
                          </Form.Item>
                        </Col>
                        <Col xs={24} md={6}>
                          <FormField
                            name="totalGB"
                            label={t('pages.clients.totalGB')}
                            tooltip={t('pages.clients.totalGBDesc')}
                            transform={{ output: (v) => Number(v) || 0 }}
                          >
                            <InputNumber min={0} step={1} style={{ width: '100%' }} />
                          </FormField>
                        </Col>
                        <Col xs={24} md={6}>
                          <Form.Item
                            label={t('pages.clients.limitIp')}
                            tooltip={t('pages.clients.limitIpDesc')}
                          >
                            <Tooltip title={limitIpNotice || undefined}>
                              <span style={{ display: 'flex', width: '100%' }}>
                                <Space.Compact style={{ display: 'flex', flex: 1 }}>
                                  <InputNumber
                                    value={limitIp}
                                    min={0}
                                    disabled={limitIpDisabled}
                                    style={{
                                      flex: 1,
                                      ...(limitIpDisabled ? { pointerEvents: 'none' } : null),
                                    }}
                                    onChange={(v) => methods.setValue('limitIp', Number(v) || 0)}
                                  />
                                  {isEdit && (
                                    <Tooltip title={t('pages.clients.ipLog')}>
                                      <Button
                                        aria-label={t('pages.clients.ipLog')}
                                        icon={<EyeOutlined />}
                                        loading={ipsLoading}
                                        onClick={openIpsModal}
                                      >
                                        {clientIps.length > 0 ? clientIps.length : ''}
                                      </Button>
                                    </Tooltip>
                                  )}
                                </Space.Compact>
                              </span>
                            </Tooltip>
                          </Form.Item>
                        </Col>
                        <Col xs={24} md={6}>
                          <Form.Item
                            label={t('pages.clients.limitHwid')}
                            tooltip={t('pages.clients.limitHwidDesc')}
                          >
                            <Space.Compact style={{ display: 'flex' }}>
                              <InputNumber
                                value={limitHwid}
                                min={0}
                                style={{ flex: 1 }}
                                onChange={(v) => methods.setValue('limitHwid', Number(v) || 0)}
                              />
                              {isEdit && (
                                <Tooltip title={t('pages.clients.hwidLog')}>
                                  <Button
                                    aria-label={t('pages.clients.hwidLog')}
                                    icon={<EyeOutlined />}
                                    loading={hwidsLoading}
                                    onClick={openHwidsModal}
                                  >
                                    {clientHwids.length > 0 ? clientHwids.length : ''}
                                  </Button>
                                </Tooltip>
                              )}
                            </Space.Compact>
                          </Form.Item>
                        </Col>
                      </Row>

                      <Row gutter={16}>
                        <Col xs={24} md={12}>
                          {delayedStart ? (
                            <FormField
                              name="delayedDays"
                              label={t('pages.clients.expireDays')}
                              transform={{ output: (v) => Number(v) || 0 }}
                            >
                              <InputNumber min={0} style={{ width: '100%' }} />
                            </FormField>
                          ) : (
                            <Form.Item label={t('pages.clients.expiryTime')}>
                              <DateTimePicker
                                value={expiryDayjs}
                                onChange={(d) =>
                                  methods.setValue('expiryDate', d ? d.valueOf() : 0)
                                }
                              />
                            </Form.Item>
                          )}
                        </Col>
                        <Col xs={12} md={6}>
                          <Form.Item label={t('pages.clients.delayedStart')}>
                            <Switch
                              checked={delayedStart}
                              onChange={(v) => {
                                methods.setValue('delayedStart', v);
                                if (v) methods.setValue('expiryDate', 0);
                                else methods.setValue('delayedDays', 0);
                              }}
                            />
                          </Form.Item>
                        </Col>
                        <Col xs={12} md={6}>
                          <FormField
                            name="reset"
                            label={t('pages.clients.renewDays')}
                            tooltip={t('pages.clients.renewDesc')}
                            transform={{ output: (v) => Number(v) || 0 }}
                          >
                            <InputNumber min={0} style={{ width: '100%' }} />
                          </FormField>
                        </Col>
                        <Col xs={12} md={6}>
                          <FormField
                            name="resetDay"
                            label={t('pages.clients.renewOnDay')}
                            tooltip={t('pages.clients.renewOnDayDesc')}
                            transform={{ output: (v) => Number(v) || 0 }}
                          >
                            <InputNumber min={0} max={31} style={{ width: '100%' }} />
                          </FormField>
                        </Col>
                        <Col xs={12} md={6}>
                          <FormField
                            name="resetMax"
                            label={t('pages.clients.renewMax')}
                            tooltip={t('pages.clients.renewMaxDesc')}
                            transform={{ output: (v) => Number(v) || 0 }}
                          >
                            <InputNumber min={0} style={{ width: '100%' }} />
                          </FormField>
                        </Col>
                        <Col xs={12} md={6}>
                          <FormField
                            name="trafficReset"
                            label={t('pages.inbounds.periodicTrafficResetTitle')}
                          >
                            <Select
                              options={TRAFFIC_RESETS.map((r) => ({
                                value: r,
                                label: t(`pages.inbounds.periodicTrafficReset.${r}`),
                              }))}
                            />
                          </FormField>
                        </Col>
                        {trafficReset === 'monthly' && (
                          <Col xs={12} md={6}>
                            <FormField
                              name="trafficResetDay"
                              label={t('pages.inbounds.periodicTrafficResetDay')}
                              transform={{ output: (v) => Number(v) || 1 }}
                            >
                              <InputNumber min={1} max={31} style={{ width: '100%' }} />
                            </FormField>
                          </Col>
                        )}
                      </Row>

                      <Row gutter={16}>
                        <Col xs={24} md={12}>
                          <FormField name="comment" label={t('pages.clients.comment')}>
                            <Input />
                          </FormField>
                        </Col>
                        <Col xs={24} md={12}>
                          <FormField
                            name="group"
                            label={t('pages.clients.group')}
                            tooltip={t('pages.clients.groupDesc')}
                            transform={{ output: (v) => v ?? '' }}
                          >
                            <AutoComplete
                              placeholder={t('pages.clients.groupPlaceholder')}
                              options={groups.map((g) => ({ value: g }))}
                              allowClear
                            />
                          </FormField>
                        </Col>
                      </Row>

                      {(tgBotEnable || showReverseTag) && (
                        <Row gutter={16}>
                          {tgBotEnable && (
                            <Col xs={24} md={12}>
                              <FormField
                                name="tgId"
                                label={t('pages.clients.telegramId')}
                                transform={{ output: (v) => Number(v) || 0 }}
                              >
                                <InputNumber
                                  min={0}
                                  controls={false}
                                  placeholder={t('pages.clients.telegramIdPlaceholder')}
                                  style={{ width: '100%' }}
                                />
                              </FormField>
                            </Col>
                          )}
                          {showReverseTag && (
                            <Col xs={24} md={12}>
                              <FormField name="reverseTag" label={t('pages.clients.reverseTag')}>
                                <Input placeholder={t('pages.clients.reverseTagPlaceholder')} />
                              </FormField>
                            </Col>
                          )}
                        </Row>
                      )}

                      <Form.Item label={t('pages.clients.attachedInbounds')} required={!isEdit}>
                        <SelectAllClearButtons
                          options={inboundOptions}
                          value={inboundIds}
                          onChange={(v) => methods.setValue('inboundIds', v)}
                        />
                        <Select
                          mode="multiple"
                          value={inboundIds}
                          onChange={(v) => methods.setValue('inboundIds', v)}
                          options={inboundOptions}
                          placeholder={t('pages.clients.selectInbound')}
                          maxTagCount="responsive"
                          placement="topLeft"
                          listHeight={220}
                          showSearch={{
                            filterOption: (input, option) =>
                              ((option?.label as string) || '')
                                .toLowerCase()
                                .includes(input.toLowerCase()),
                          }}
                        />
                      </Form.Item>

                      <Form.Item>
                        <Switch
                          aria-label={t('enable')}
                          checked={enable}
                          onChange={(v) => methods.setValue('enable', v)}
                        />
                        <span style={{ marginLeft: 8 }}>{t('enable')}</span>
                      </Form.Item>
                    </>
                  ),
                },
                {
                  key: 'config',
                  label: t('pages.clients.tabCredentials'),
                  children: (
                    <>
                      <Form.Item label={t('pages.clients.uuid')}>
                        <Space.Compact style={{ display: 'flex' }}>
                          <Input
                            value={uuid}
                            style={{ flex: 1 }}
                            onChange={(e) => methods.setValue('uuid', e.target.value)}
                          />
                          <Button
                            aria-label={t('regenerate')}
                            icon={<ReloadOutlined />}
                            onClick={() => methods.setValue('uuid', RandomUtil.randomUUID())}
                          />
                        </Space.Compact>
                      </Form.Item>

                      <Form.Item
                        label={t('pages.clients.password')}
                        tooltip={t('pages.clients.passwordDesc')}
                      >
                        <Space.Compact style={{ display: 'flex' }}>
                          <Input
                            value={password}
                            style={{ flex: 1 }}
                            onChange={(e) => methods.setValue('password', e.target.value)}
                          />
                          <Button
                            aria-label={t('regenerate')}
                            icon={<ReloadOutlined />}
                            onClick={regeneratePassword}
                          />
                        </Space.Compact>
                      </Form.Item>

                      <Form.Item label={t('pages.clients.subId')}>
                        <Space.Compact style={{ display: 'flex' }}>
                          <Input
                            value={subId}
                            style={{ flex: 1 }}
                            onChange={(e) => methods.setValue('subId', e.target.value)}
                          />
                          <Button
                            aria-label={t('regenerate')}
                            icon={<ReloadOutlined />}
                            onClick={() =>
                              methods.setValue('subId', RandomUtil.randomLowerAndNum(16))
                            }
                          />
                        </Space.Compact>
                      </Form.Item>

                      <Form.Item
                        label={t('pages.clients.hysteriaAuth')}
                        tooltip={t('pages.clients.hysteriaAuthDesc')}
                      >
                        <Space.Compact style={{ display: 'flex' }}>
                          <Input
                            value={auth}
                            style={{ flex: 1 }}
                            onChange={(e) => methods.setValue('auth', e.target.value)}
                          />
                          <Button
                            aria-label={t('regenerate')}
                            icon={<ReloadOutlined />}
                            onClick={() =>
                              methods.setValue('auth', RandomUtil.randomLowerAndNum(16))
                            }
                          />
                        </Space.Compact>
                      </Form.Item>

                      {showFlow && (
                        <FormField name="flow" label={t('pages.clients.flow')}>
                          <Select
                            options={[
                              { value: '', label: t('none') },
                              ...FLOW_OPTIONS.map((k) => ({ value: k, label: k })),
                            ]}
                          />
                        </FormField>
                      )}
                      {showSecurity && (
                        <FormField name="security" label={t('pages.clients.vmessSecurity')}>
                          <Select
                            options={VMESS_SECURITY_OPTIONS.map((k) => ({ value: k, label: k }))}
                          />
                        </FormField>
                      )}
                      {showWireguard && (
                        <>
                          <Form.Item label={t('pages.clients.wireguardPrivateKey')}>
                            <Space.Compact style={{ display: 'flex' }}>
                              <Input
                                value={wgPrivateKey}
                                style={{ flex: 1 }}
                                onChange={(e) => {
                                  const priv = e.target.value;
                                  methods.setValue('wgPrivateKey', priv);
                                  methods.setValue(
                                    'wgPublicKey',
                                    priv ? Wireguard.generateKeypair(priv).publicKey : '',
                                  );
                                }}
                              />
                              <Button
                                aria-label={t('regenerate')}
                                icon={<ReloadOutlined />}
                                onClick={regenerateWireguardKeys}
                              />
                            </Space.Compact>
                          </Form.Item>
                          <FormField
                            name="wgPublicKey"
                            label={t('pages.clients.wireguardPublicKey')}
                          >
                            <Input disabled />
                          </FormField>
                          <FormField
                            name="wgPreSharedKey"
                            label={t('pages.clients.wireguardPreSharedKey')}
                          >
                            <Input />
                          </FormField>
                          <FormField
                            name="wgAllowedIPs"
                            label={t('pages.clients.wireguardAllowedIPs')}
                            extra={t('pages.clients.wireguardAllowedIPsHint')}
                          >
                            <Input placeholder="10.0.0.2/32" />
                          </FormField>
                        </>
                      )}
                      {showMtproto && (
                        <>
                          <Form.Item
                            label={t('pages.clients.mtprotoSecret')}
                            extra={t('pages.clients.mtprotoSecretHint')}
                          >
                            <Space.Compact style={{ display: 'flex' }}>
                              <Input
                                value={secret}
                                style={{ flex: 1 }}
                                onChange={(e) => methods.setValue('secret', e.target.value)}
                              />
                              <Button
                                aria-label={t('regenerate')}
                                icon={<ReloadOutlined />}
                                onClick={regenerateMtprotoSecret}
                              />
                            </Space.Compact>
                          </Form.Item>
                          <FormField
                            name="adTag"
                            label={t('pages.clients.mtprotoAdTag')}
                            extra={t('pages.clients.mtprotoAdTagHint')}
                          >
                            <Input allowClear placeholder="0123456789abcdef0123456789abcdef" />
                          </FormField>
                        </>
                      )}
                    </>
                  ),
                },
                {
                  key: 'nodeQuotas',
                  label: t('pages.clients.nodeQuotas', { defaultValue: 'Per-node limits' }),
                  children: (
                    <>
                      <Typography.Paragraph type="secondary" style={{ marginTop: 4 }}>
                        {t('pages.clients.nodeQuotasHint', {
                          defaultValue:
                            'Limits apply to the physical node. 0 means unlimited; a node quota never disables the whole client.',
                        })}
                      </Typography.Paragraph>
                      {nodeQuotaError && (
                        <Typography.Paragraph type="danger">
                          {nodeQuotaError}{' '}
                          {t('pages.clients.nodeQuotaSaveDisabled', {
                            defaultValue: 'Save is disabled until node quotas can be loaded.',
                          })}
                        </Typography.Paragraph>
                      )}
                      {isEdit && resetAllNodeQuotas && nodeQuotaRows.length > 0 && (
                        <div style={{ marginBottom: 12 }}>
                          <Button
                            icon={<RetweetOutlined />}
                            loading={nodeQuotaBusy === 'all'}
                            disabled={nodeQuotaBusy !== null || nodeQuotaDirty.current}
                            onClick={onResetAllNodeQuotas}
                          >
                            {t('pages.clients.resetAllNodeQuotas', {
                              defaultValue: 'Reset all node traffic',
                            })}
                          </Button>
                        </div>
                      )}
                      {nodeQuotaLoading ? (
                        <div style={{ display: 'flex', justifyContent: 'center', padding: 24 }}>
                          <Spin />
                        </div>
                      ) : nodeQuotaRows.length === 0 ? (
                        <Typography.Text type="secondary">
                          {t('pages.clients.noNodes', {
                            defaultValue: 'No physical nodes available.',
                          })}
                        </Typography.Text>
                      ) : (
                        <div style={{ display: 'grid', gap: 10 }}>
                          {nodeQuotaRows.map((row) => {
                            const usedBytes = row.up + row.down;
                            const usedGB = bytesToQuotaGB(usedBytes);
                            const exceeded =
                              row.totalGB > 0 && usedBytes >= quotaGBToBytes(row.totalGB);
                            return (
                              <Card
                                key={row.nodeId}
                                size="small"
                                styles={{ body: { padding: 12 } }}
                              >
                                <Row gutter={[12, 8]} align="middle">
                                  <Col xs={24} md={7}>
                                    <Typography.Text strong>{row.nodeName}</Typography.Text>
                                    <div>
                                      <Tag color={row.blocked || exceeded ? 'red' : 'green'}>
                                        {usedGB.toFixed(2)} GB /{' '}
                                        {row.totalGB > 0 ? `${row.totalGB} GB` : '∞'}
                                      </Tag>
                                      {row.blocked && (
                                        <Tag color="red">
                                          {t('pages.clients.quotaExceeded', {
                                            defaultValue: 'Exceeded',
                                          })}
                                        </Tag>
                                      )}
                                    </div>
                                    {row.lastError && (
                                      <Typography.Text type="danger" ellipsis>
                                        {row.lastError}
                                      </Typography.Text>
                                    )}
                                  </Col>
                                  <Col xs={12} md={5}>
                                    <Typography.Text type="secondary">
                                      {t('pages.clients.nodeLimitGB', {
                                        defaultValue: 'Limit (GB)',
                                      })}
                                    </Typography.Text>
                                    <InputNumber
                                      min={0}
                                      step={0.01}
                                      precision={6}
                                      value={row.totalGB}
                                      style={{ width: '100%' }}
                                      onChange={(value) =>
                                        updateNodeQuotaRow(row.nodeId, {
                                          totalGB: Number(value) || 0,
                                        })
                                      }
                                    />
                                  </Col>
                                  <Col xs={12} md={5}>
                                    <Typography.Text type="secondary">
                                      {t('pages.clients.resetPolicy', { defaultValue: 'Reset' })}
                                    </Typography.Text>
                                    <Select
                                      value={row.resetPolicy}
                                      style={{ width: '100%' }}
                                      options={[
                                        'never',
                                        'hourly',
                                        'daily',
                                        'weekly',
                                        'monthly',
                                      ].map((value) => ({
                                        value,
                                        label: value[0].toUpperCase() + value.slice(1),
                                      }))}
                                      onChange={(value) =>
                                        updateNodeQuotaRow(row.nodeId, { resetPolicy: value })
                                      }
                                    />
                                  </Col>
                                  <Col xs={12} md={4}>
                                    <Typography.Text type="secondary">
                                      {t('pages.clients.resetDay', { defaultValue: 'Day' })}
                                    </Typography.Text>
                                    <InputNumber
                                      min={1}
                                      max={31}
                                      value={row.resetDay}
                                      disabled={row.resetPolicy === 'never'}
                                      style={{ width: '100%' }}
                                      onChange={(value) =>
                                        updateNodeQuotaRow(row.nodeId, {
                                          resetDay: Number(value) || 1,
                                        })
                                      }
                                    />
                                  </Col>
                                  {isEdit && resetNodeQuota && (
                                    <Col xs={12} md={3}>
                                      <Button
                                        block
                                        icon={<RetweetOutlined />}
                                        loading={nodeQuotaBusy === row.nodeId}
                                        disabled={nodeQuotaBusy !== null || nodeQuotaDirty.current}
                                        onClick={() => onResetNodeQuota(row.nodeId)}
                                      >
                                        {t('reset')}
                                      </Button>
                                    </Col>
                                  )}
                                </Row>
                              </Card>
                            );
                          })}
                        </div>
                      )}
                    </>
                  ),
                },
                {
                  key: 'links',
                  label: t('pages.clients.tabLinks'),
                  children: (
                    <>
                      <Typography.Paragraph type="secondary" style={{ marginTop: 4 }}>
                        {t('pages.clients.linksHint')}
                      </Typography.Paragraph>

                      <Button
                        type="primary"
                        icon={<PlusOutlined />}
                        onClick={() => addExternalLinkRow('link')}
                      >
                        {t('pages.clients.addExternalLink')}
                      </Button>
                      <div style={{ marginTop: 12, marginBottom: 24 }}>
                        {linkRows.length === 0 ? (
                          <Typography.Text type="secondary">
                            {t('pages.clients.noExternalLinks')}
                          </Typography.Text>
                        ) : (
                          linkRows.map(({ field, index }) => (
                            <div key={field.id} className="external-link-card">
                              <div className="external-link-row">
                                <div className="external-link-enable">
                                  <FormField
                                    name={`externalLinks.${index}.enable`}
                                    valueProp="checked"
                                    noStyle
                                  >
                                    <Switch size="small" />
                                  </FormField>
                                  <span>{t('enable')}</span>
                                </div>
                                <FormField name={`externalLinks.${index}.value`} noStyle>
                                  <Input
                                    aria-label="vless:// · vmess:// · trojan:// · ss:// · hysteria2:// · wireguard://"
                                    placeholder="vless:// · vmess:// · trojan:// · ss:// · hysteria2:// · wireguard://"
                                  />
                                </FormField>
                                <Tooltip title={t('delete')}>
                                  <Button
                                    aria-label={t('delete')}
                                    danger
                                    icon={<DeleteOutlined />}
                                    onClick={() => removeExternalLink(index)}
                                  />
                                </Tooltip>
                              </div>
                              <div className="external-link-details two-cols">
                                <FormField name={`externalLinks.${index}.remark`} noStyle>
                                  <Input aria-label={t('remark')} placeholder={t('remark')} />
                                </FormField>
                                <Controller
                                  control={methods.control}
                                  name={`externalLinks.${index}.expiryTime`}
                                  render={({ field: expiryField }) => (
                                    <DateTimePicker
                                      value={
                                        Number(expiryField.value) > 0
                                          ? dayjs(Number(expiryField.value))
                                          : null
                                      }
                                      onChange={(v) => expiryField.onChange(v ? v.valueOf() : 0)}
                                      placeholder={t('pages.inbounds.leaveBlankToNeverExpire')}
                                    />
                                  )}
                                />
                              </div>
                            </div>
                          ))
                        )}
                      </div>

                      <Button
                        type="primary"
                        icon={<PlusOutlined />}
                        onClick={() => addExternalLinkRow('subscription')}
                      >
                        {t('pages.clients.addExternalSubscription')}
                      </Button>
                      <div style={{ marginTop: 12 }}>
                        {subscriptionRows.length === 0 ? (
                          <Typography.Text type="secondary">
                            {t('pages.clients.noExternalSubscriptions')}
                          </Typography.Text>
                        ) : (
                          subscriptionRows.map(({ field, index }) => (
                            <div key={field.id} className="external-link-card">
                              <div className="external-link-row">
                                <div className="external-link-enable">
                                  <FormField
                                    name={`externalLinks.${index}.enable`}
                                    valueProp="checked"
                                    noStyle
                                  >
                                    <Switch size="small" />
                                  </FormField>
                                  <span>{t('enable')}</span>
                                </div>
                                <FormField name={`externalLinks.${index}.value`} noStyle>
                                  <Input
                                    aria-label="https://provider.example/sub/…"
                                    placeholder="https://provider.example/sub/…"
                                  />
                                </FormField>
                                <Tooltip title={t('delete')}>
                                  <Button
                                    aria-label={t('delete')}
                                    danger
                                    icon={<DeleteOutlined />}
                                    onClick={() => removeExternalLink(index)}
                                  />
                                </Tooltip>
                              </div>
                              <div className="external-link-details three-cols">
                                <FormField name={`externalLinks.${index}.remark`} noStyle>
                                  <Input aria-label={t('remark')} placeholder={t('remark')} />
                                </FormField>
                                <FormField name={`externalLinks.${index}.namePrefix`} noStyle>
                                  <Input
                                    aria-label={t('pages.clients.namePrefix')}
                                    placeholder={t('pages.clients.namePrefix')}
                                  />
                                </FormField>
                                <Controller
                                  control={methods.control}
                                  name={`externalLinks.${index}.expiryTime`}
                                  render={({ field: expiryField }) => (
                                    <DateTimePicker
                                      value={
                                        Number(expiryField.value) > 0
                                          ? dayjs(Number(expiryField.value))
                                          : null
                                      }
                                      onChange={(v) => expiryField.onChange(v ? v.valueOf() : 0)}
                                      placeholder={t('pages.inbounds.leaveBlankToNeverExpire')}
                                    />
                                  )}
                                />
                              </div>
                              <Typography.Text
                                type={field.lastFetchError ? 'danger' : 'secondary'}
                                className="external-link-fetch-status"
                              >
                                {field.lastFetchError
                                  ? `${t('pages.clients.lastFetchError')}: ${field.lastFetchError}`
                                  : field.lastFetchAt > 0
                                    ? `${t('pages.clients.lastFetchAt')}: ${dayjs(field.lastFetchAt).format('YYYY-MM-DD HH:mm:ss')}`
                                    : t('pages.clients.neverFetched')}
                              </Typography.Text>
                            </div>
                          ))
                        )}
                      </div>
                    </>
                  ),
                },
              ]}
            />
          </Form>
        </FormProvider>
      </Modal>

      <Modal
        open={ipsModalOpen}
        title={`${t('pages.clients.ipLog')}${client?.email ? ` — ${client.email}` : ''}`}
        width={440}
        zIndex={CLIENT_IP_LOG_MODAL_Z_INDEX}
        onCancel={() => setIpsModalOpen(false)}
        footer={[
          <Button key="refresh" icon={<ReloadOutlined />} loading={ipsLoading} onClick={loadIps}>
            {t('refresh')}
          </Button>,
          <Button
            key="clear"
            danger
            loading={ipsClearing}
            disabled={clientIps.length === 0}
            onClick={clearIps}
          >
            {t('pages.clients.clearAll')}
          </Button>,
          <Button key="close" type="primary" onClick={() => setIpsModalOpen(false)}>
            {t('close')}
          </Button>,
        ]}
      >
        {clientIps.length > 0 ? (
          <div style={{ maxHeight: 360, overflowY: 'auto' }}>
            {clientIps.map((entry, idx) => (
              <Tag
                key={idx}
                color="blue"
                style={{
                  display: 'block',
                  width: 'fit-content',
                  maxWidth: '100%',
                  marginBottom: 6,
                  padding: '2px 8px',
                  fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
                }}
              >
                {entry.ip}
                {entry.time ? ` (${entry.time})` : ''}
                {entry.node ? (
                  <span style={{ marginInlineStart: 6, opacity: 0.85, fontWeight: 600 }}>
                    @ {entry.node}
                  </span>
                ) : null}
              </Tag>
            ))}
          </div>
        ) : (
          <Tag>{t('tgbot.noIpRecord')}</Tag>
        )}
      </Modal>

      <ClientHwidListModal
        open={hwidsModalOpen}
        email={client?.email}
        zIndex={CLIENT_IP_LOG_MODAL_Z_INDEX}
        hwids={clientHwids}
        loading={hwidsLoading}
        clearing={hwidsClearing}
        deletingId={deletingHwidId}
        formatDate={hwidDateLabel}
        onRefresh={loadHwids}
        onClearAll={clearHwids}
        onDelete={deleteHwid}
        onClose={() => setHwidsModalOpen(false)}
      />
    </>
  );
}
