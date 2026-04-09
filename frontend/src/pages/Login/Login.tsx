import { useState } from 'react';
import { Alert, Button, Card, Form, Input, Typography, message } from 'antd';
import { useNavigate } from 'react-router-dom';
import { login, useAuthStatus } from '@/hooks';

const { Title, Text } = Typography;

interface LoginFormValues {
  username: string;
  password: string;
}

export function Login() {
  const navigate = useNavigate();
  const [submitting, setSubmitting] = useState(false);
  const { data: authStatus } = useAuthStatus();

  const handleSubmit = async (values: LoginFormValues) => {
    setSubmitting(true);
    try {
      await login(values.username, values.password);
      message.success('登录成功');
      navigate('/', { replace: true });
    } catch (err) {
      message.error(err instanceof Error ? err.message : '登录失败');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-slate-100 p-4">
      <Card className="w-full max-w-md shadow-sm">
        <div className="mb-6 text-center">
          <Title level={3} className="!mb-1">WorkFlow Deck</Title>
          <Text type="secondary">请登录后继续使用</Text>
        </div>

        {authStatus && !authStatus.auth_enabled && (
          <Alert
            type="info"
            className="mb-4"
            message="当前服务未启用认证，登录页仅用于兼容跳转。"
          />
        )}

        <Form<LoginFormValues> layout="vertical" onFinish={handleSubmit}>
          <Form.Item
            name="username"
            label="用户名"
            rules={[{ required: true, message: '请输入用户名' }]}
          >
            <Input autoFocus autoComplete="username" placeholder="请输入用户名" />
          </Form.Item>
          <Form.Item
            name="password"
            label="密码"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password autoComplete="current-password" placeholder="请输入密码" />
          </Form.Item>
          <Form.Item className="mb-0">
            <Button type="primary" htmlType="submit" block loading={submitting}>
              登录
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
