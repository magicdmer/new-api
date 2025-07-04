import React, { useEffect, useState, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import {
  API,
  isMobile,
  showError,
  showSuccess,
  renderQuota,
  renderQuotaWithPrompt,
} from '../../helpers';
import {
  Button,
  Modal,
  SideSheet,
  Space,
  Spin,
  Typography,
  Card,
  Tag,
  Form,
  Avatar,
  Row,
  Col,
  Input,
} from '@douyinfe/semi-ui';
import {
  IconUser,
  IconSave,
  IconClose,
  IconLink,
  IconUserGroup,
  IconPlus,
} from '@douyinfe/semi-icons';

const { Text, Title } = Typography;

const EditUser = (props) => {
  const { t } = useTranslation();
  const userId = props.editingUser.id;
  const [loading, setLoading] = useState(true);
  const [addQuotaModalOpen, setIsModalOpen] = useState(false);
  const [addQuotaLocal, setAddQuotaLocal] = useState('0');
  const [groupOptions, setGroupOptions] = useState([]);
  const formApiRef = useRef(null);

  const isEdit = Boolean(userId);

  const getInitValues = () => ({
    username: '',
    display_name: '',
    password: '',
    github_id: '',
    oidc_id: '',
    wechat_id: '',
    telegram_id: '',
    email: '',
    quota: 0,
    group: 'default',
    remark: '',
  });

  const [unlimitedQuota, setUnlimitedQuota] = useState(false);
  const handleInputChange = (name, value) => {
    setInputs((inputs) => ({ ...inputs, [name]: value }));
    if (name === 'unlimited_quota') {
      setUnlimitedQuota(value);
      if (value) {
        setInputs((inputs) => ({ ...inputs, quota: 0 }));
      }
    }
  };
  const fetchGroups = async () => {
    try {
      let res = await API.get(`/api/group/`);
      setGroupOptions(
        res.data.data.map((g) => ({ label: g, value: g }))
      );
    } catch (e) {
      showError(e.message);
    }
  };

  const handleCancel = () => props.handleClose();

  const loadUser = async () => {
    setLoading(true);
    const url = userId ? `/api/user/${userId}` : `/api/user/self`;
    const res = await API.get(url);
    const { success, message, data } = res.data;
    if (success) {
      data.password = '';
      formApiRef.current?.setValues({ ...getInitValues(), ...data });
      setUnlimitedQuota(data.unlimited_quota);
    } else {
      showError(message);
    }
    setLoading(false);
  };

  useEffect(() => {
    loadUser();
    if (userId) fetchGroups();
  }, [props.editingUser.id]);

  /* ----------------------- submit ----------------------- */
  const submit = async (values) => {
    setLoading(true);
    let payload = { ...values };
    if (typeof payload.quota === 'string') payload.quota = parseInt(payload.quota) || 0;
    if (userId) {
      payload.id = parseInt(userId);
      }
    const url = userId ? `/api/user/` : `/api/user/self`;
    const res = await API.put(url, payload);
    const { success, message } = res.data;
    if (success) {
      showSuccess(t('用户信息更新成功！'));
      props.refresh();
      props.handleClose();
    } else {
      showError(message);
    }
    setLoading(false);
  };

  /* --------------------- quota helper -------------------- */
  const addLocalQuota = () => {
    const current = parseInt(formApiRef.current?.getValue('quota') || 0);
    const delta = parseInt(addQuotaLocal) || 0;
    formApiRef.current?.setValue('quota', current + delta);
  };

  /* --------------------------- UI --------------------------- */
  return (
    <>
      <SideSheet
        placement='right'
        title={
          <Space>
            <Tag color='blue' shape='circle'>
              {t(isEdit ? '编辑' : '新建')}
            </Tag>
            <Title heading={4} className='m-0'>
              {isEdit ? t('编辑用户') : t('创建用户')}
            </Title>
          </Space>
        }
        bodyStyle={{ padding: 0 }}
        visible={props.visible}
        width={isMobile() ? '100%' : 600}
        footer={
          <div className='flex justify-end bg-white'>
            <Space>
              <Button
                theme='solid'
                onClick={() => formApiRef.current?.submitForm()}
                icon={<IconSave />}
                loading={loading}
              >
                {t('提交')}
              </Button>
              <Button
                theme='light'
                type='primary'
                onClick={handleCancel}
                icon={<IconClose />}
              >
                {t('取消')}
              </Button>
            </Space>
          </div>
        }
        closeIcon={null}
        onCancel={handleCancel}
      >
        <Spin spinning={loading}>
          <Form
            initValues={getInitValues()}
            getFormApi={(api) => (formApiRef.current = api)}
            onSubmit={submit}
          >
            {({ values }) => (
              <div className='p-2'>
                {/* 基本信息 */}
                <Card className='!rounded-2xl shadow-sm border-0'>
                  <div className='flex items-center mb-2'>
                    <Avatar size='small' color='blue' className='mr-2 shadow-md'>
                      <IconUser size={16} />
                    </Avatar>
                    <div>
                      <Text className='text-lg font-medium'>{t('基本信息')}</Text>
                      <div className='text-xs text-gray-600'>{t('用户的基本账户信息')}</div>
                </div>
                </div>

                  <Row gutter={12}>
                    <Col span={24}>
                      <Form.Input
                        field='username'
                        label={t('用户名')}
                    placeholder={t('请输入新的用户名')}
                        rules={[{ required: true, message: t('请输入用户名') }]}
                    showClear
                  />
                    </Col>

                    <Col span={24}>
                      <Form.Input
                        field='password'
                        label={t('密码')}
                    placeholder={t('请输入新的密码，最短 8 位')}
                        mode='password'
                        showClear
                  />
                    </Col>

                    <Col span={24}>
                      <Form.Input
                        field='display_name'
                        label={t('显示名称')}
                    placeholder={t('请输入新的显示名称')}
                    showClear
                  />
                    </Col>

                    <Col span={24}>
                      <Form.Input
                        field='remark'
                        label={t('备注')}
                    placeholder={t('请输入备注（仅管理员可见）')}
                    showClear
                  />
                    </Col>
                  </Row>
            </Card>

                {/* 权限设置 */}
            {userId && (
                  <Card className='!rounded-2xl shadow-sm border-0'>
                    <div className='flex items-center mb-2'>
                      <Avatar size='small' color='green' className='mr-2 shadow-md'>
                        <IconUserGroup size={16} />
                      </Avatar>
                      <div>
                        <Text className='text-lg font-medium'>{t('权限设置')}</Text>
                        <div className='text-xs text-gray-600'>{t('用户分组和额度管理')}</div>
                  </div>
                  </div>

                    <Row gutter={12}>
                      <Col span={24}>
                        <Form.Select
                          field='group'
                          label={t('分组')}
                      placeholder={t('请选择分组')}
                          optionList={groupOptions}
                          allowAdditions
                      search
                          rules={[{ required: true, message: t('请选择分组') }]}
                    />
                      </Col>

                  <div>
                    <div className="flex justify-between mb-2">
                      <Text strong>{t('剩余额度')}</Text>
                      <Text type="tertiary">{renderQuotaWithPrompt(quota)}</Text>
                    </div>
                    <div style={{ marginTop: 20 }}>
                      <Typography.Text>{t('额度设置')}</Typography.Text>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginTop: 8 }}>
                        <Space>
                          <Input
                            name='quota'
                          placeholder={t('请输入新的剩余额度')}
                            onChange={(value) => handleInputChange('quota', value)}
                            value={quota}
                            type={'number'}
                            autoComplete='new-password'
                            disabled={unlimitedQuota}
                          />
                          <Button onClick={openAddQuotaModal} disabled={unlimitedQuota}>
                            {t('添加额度')}
                          </Button>
                          <Button
                            type={unlimitedQuota ? 'primary' : 'default'}
                            onClick={() => handleInputChange('unlimited_quota', !unlimitedQuota)}
                          >
                            {t('无限额度')}
                          </Button>
                        </Space>
                      </div>
                    </div>
                  </div>
                </div>
              </Card>
            )}

                {/* 绑定信息 */}
                <Card className='!rounded-2xl shadow-sm border-0'>
                  <div className='flex items-center mb-2'>
                    <Avatar size='small' color='purple' className='mr-2 shadow-md'>
                      <IconLink size={16} />
                    </Avatar>
                <div>
                      <Text className='text-lg font-medium'>{t('绑定信息')}</Text>
                      <div className='text-xs text-gray-600'>{t('第三方账户绑定状态（只读）')}</div>
                </div>
                </div>

                  <Row gutter={12}>
                    {['github_id', 'oidc_id', 'wechat_id', 'email', 'telegram_id'].map((field) => (
                      <Col span={24} key={field}>
                        <Form.Input
                          field={field}
                          label={t(`已绑定的 ${field.replace('_id', '').toUpperCase()} 账户`)}
                    readonly
                          placeholder={t('此项只读，需要用户通过个人设置页面的相关绑定按钮进行绑定，不可直接修改')}
                  />
                      </Col>
                    ))}
                  </Row>
                </Card>
                </div>
                    )}
          </Form>
        </Spin>
      </SideSheet>

      {/* 添加额度模态框 */}
      <Modal
        centered
        visible={addQuotaModalOpen}
        onOk={() => {
          addLocalQuota();
          setIsModalOpen(false);
        }}
        onCancel={() => setIsModalOpen(false)}
        closable={null}
        title={
          <div className='flex items-center'>
            <IconPlus className='mr-2' />
            {t('添加额度')}
          </div>
        }
      >
        <div className='mb-4'>
          {
            (() => {
              const current = formApiRef.current?.getValue('quota') || 0;
              return (
                <Text type='secondary' className='block mb-2'>
                  {`${t('新额度')}${renderQuota(current)} + ${renderQuota(addQuotaLocal)} = ${renderQuota(current + parseInt(addQuotaLocal || 0))}`}
          </Text>
              );
            })()
          }
        </div>
        <Input
          placeholder={t('需要添加的额度（支持负数）')}
          type='number'
          value={addQuotaLocal}
          onChange={setAddQuotaLocal}
          showClear
        />
      </Modal>
    </>
  );
};

export default EditUser;
