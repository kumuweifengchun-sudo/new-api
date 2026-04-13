/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  Button,
  Form,
  Pagination,
  SideSheet,
  Space,
  Spin,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconClose, IconSearch } from '@douyinfe/semi-icons';
import { API, showError } from '../../../../helpers';
import { timestamp2string } from '../../../../helpers/utils';
import { DATE_RANGE_PRESETS } from '../../../../constants/console.constants';
import CardTable from '../../../common/ui/CardTable';

const { Text, Title } = Typography;

const formatTimestamp = (timestamp) => {
  if (!timestamp) {
    return '-';
  }
  return new Date(timestamp * 1000).toLocaleString();
};

const getDefaultFilters = () => {
  const now = Math.floor(Date.now() / 1000);
  return {
    dateRange: [
      timestamp2string(now - 86400 * 7),
      timestamp2string(now + 3600),
    ],
    token_id: undefined,
  };
};

const UserRecentIpsModal = ({ visible, onCancel, userId, username, t, isMobile }) => {
  const formApiRef = useRef(null);
  const [loading, setLoading] = useState(false);
  const [tokenOptions, setTokenOptions] = useState([]);
  const [items, setItems] = useState([]);
  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [total, setTotal] = useState(0);

  const loadTokenOptions = async () => {
    if (!userId) {
      return;
    }
    try {
      const res = await API.get(`/api/user/${userId}/tokens`);
      const { success, message, data } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      const options = (data || []).map((token) => ({
        label: token.name ? `${token.name} (#${token.id})` : `#${token.id}`,
        value: token.id,
      }));
      setTokenOptions(options);
    } catch (error) {
      showError(error.message);
    }
  };

  const loadRecentIPs = async (page = currentPage, size = pageSize, values) => {
    if (!userId) {
      return;
    }
    const formValues = values || formApiRef.current?.getValues() || getDefaultFilters();
    let startTimestamp = 0;
    let endTimestamp = 0;
    if (Array.isArray(formValues.dateRange) && formValues.dateRange.length === 2) {
      startTimestamp = parseInt(Date.parse(formValues.dateRange[0]) / 1000);
      endTimestamp = parseInt(Date.parse(formValues.dateRange[1]) / 1000);
    }

    let url = `/api/log/users/${userId}/recent-ips?p=${page}&page_size=${size}&start_timestamp=${startTimestamp}&end_timestamp=${endTimestamp}`;
    if (formValues.token_id) {
      url += `&token_id=${formValues.token_id}`;
    }

    setLoading(true);
    try {
      const res = await API.get(url);
      const { success, message, data } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      setItems((data?.items || []).map((item) => ({ ...item, key: item.ip })));
      setCurrentPage(data?.page || page);
      setPageSize(data?.page_size || size);
      setTotal(data?.total || 0);
    } catch (error) {
      showError(error.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!visible || !userId) {
      return;
    }
    const defaultFilters = getDefaultFilters();
    formApiRef.current?.setValues(defaultFilters);
    setCurrentPage(1);
    setPageSize(10);
    loadTokenOptions();
    loadRecentIPs(1, 10, defaultFilters);
  }, [visible, userId]);

  const columns = useMemo(
    () => [
      {
        title: t('IP'),
        dataIndex: 'ip',
        render: (text) => (
          <Tag
            color='orange'
            shape='circle'
            copyable
          >
            {text}
          </Tag>
        ),
      },
      {
        title: t('最近使用时间'),
        dataIndex: 'last_used_at',
        render: (text) => formatTimestamp(text),
      },
      {
        title: t('请求次数'),
        dataIndex: 'request_count',
      },
      {
        title: t('涉及令牌数'),
        dataIndex: 'token_count',
      },
      {
        title: t('关联令牌'),
        dataIndex: 'tokens',
        render: (tokens) => {
          if (!tokens || tokens.length === 0) {
            return <Text type='tertiary'>-</Text>;
          }
          return (
            <div className='flex flex-wrap gap-1 justify-end md:justify-start'>
              {tokens.map((token) => (
                <Tag
                  key={`${token.id}-${token.name}`}
                  color='white'
                  shape='circle'
                  onClick={() => {
                    formApiRef.current?.setValue('token_id', token.id);
                    loadRecentIPs(1, pageSize, {
                      ...(formApiRef.current?.getValues() || getDefaultFilters()),
                      token_id: token.id,
                    });
                  }}
                >
                  {token.name ? `${token.name} (#${token.id})` : `#${token.id}`}
                </Tag>
              ))}
            </div>
          );
        },
      },
    ],
    [pageSize, t],
  );

  return (
    <SideSheet
      placement='right'
      visible={visible}
      width={isMobile ? '100%' : 760}
      closeIcon={null}
      bodyStyle={{ padding: 0 }}
      onCancel={onCancel}
      title={
        <Space>
          <Tag color='blue' shape='circle'>
            {t('最近活跃 IP')}
          </Tag>
          <Title heading={4} className='m-0'>
            {username ? `${username} · ${t('最近活跃 IP')}` : t('最近活跃 IP')}
          </Title>
        </Space>
      }
      footer={
        <div className='flex justify-end bg-white'>
          <Button theme='light' type='primary' onClick={onCancel} icon={<IconClose />}>
            {t('关闭')}
          </Button>
        </div>
      }
    >
      <div className='p-4 space-y-4'>
        <div>
          <Text type='secondary'>
            {t('仅统计消费和错误日志中已记录 IP 的请求。历史数据不会回填。')}
          </Text>
        </div>

        <Form
          initValues={getDefaultFilters()}
          getFormApi={(api) => {
            formApiRef.current = api;
          }}
          onSubmit={() => loadRecentIPs(1, pageSize)}
          allowEmpty
          autoComplete='off'
          layout='vertical'
        >
          <div className='grid grid-cols-1 md:grid-cols-3 gap-3'>
            <Form.DatePicker
              field='dateRange'
              type='dateTimeRange'
              className='md:col-span-2'
              placeholder={[t('开始时间'), t('结束时间')]}
              presets={DATE_RANGE_PRESETS.map((preset) => ({
                text: t(preset.text),
                start: preset.start(),
                end: preset.end(),
              }))}
              showClear
            />
            <Form.Select
              field='token_id'
              optionList={tokenOptions}
              filter
              search
              showClear
              placeholder={t('筛选令牌')}
            />
          </div>
          <div className='mt-3 flex flex-wrap gap-2'>
            <Button
              theme='solid'
              icon={<IconSearch />}
              loading={loading}
              onClick={() => loadRecentIPs(1, pageSize)}
            >
              {t('查询')}
            </Button>
            <Button
              theme='light'
              onClick={() => {
                const defaultFilters = getDefaultFilters();
                formApiRef.current?.setValues(defaultFilters);
                loadRecentIPs(1, pageSize, defaultFilters);
              }}
            >
              {t('重置')}
            </Button>
          </div>
        </Form>

        <Spin spinning={loading}>
          <CardTable
            rowKey='key'
            hidePagination
            columns={columns}
            dataSource={items}
          />
        </Spin>

        <div className='flex justify-end'>
          <Pagination
            currentPage={currentPage}
            pageSize={pageSize}
            total={total}
            onPageChange={(page) => loadRecentIPs(page, pageSize)}
            onPageSizeChange={(size) => loadRecentIPs(1, size)}
            showSizeChanger
            pageSizeOpts={[10, 20, 50]}
          />
        </div>
      </div>
    </SideSheet>
  );
};

export default UserRecentIpsModal;
