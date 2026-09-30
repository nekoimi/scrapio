import axios, {AxiosInstance} from 'axios';
import {ElMessage, ElMessageBox} from 'element-plus';
import {Session} from '/@/utils/storage';
import qs from 'qs';

// 配置新建一个 axios 实例
const service: AxiosInstance = axios.create({
    baseURL: import.meta.env.VITE_API_URL,
    timeout: 50000,
    headers: {'Content-Type': 'application/json'},
    paramsSerializer: {
        serialize(params) {
            return qs.stringify(params, {allowDots: true});
        },
    },
});

// 添加请求拦截器
service.interceptors.request.use(
    (config) => {
        // 在发送请求之前做些什么 token
        if (Session.get('token')) {
            config.headers!['Authorization'] = `${Session.get('token')}`;
        }
        return config;
    },
    (error) => {
        // 对请求错误做些什么
        return Promise.reject(error);
    }
);

// 添加响应拦截器
service.interceptors.response.use(
    (response) => {
        // 对响应数据做点什么
        const res = response.data;
        if (res.code && res.code !== 0) {
            // 认证信息异常
            if (res.code === 401 || res.code === 20401) {
                ElMessageBox.alert(`${res.msg}，请重新登录`, '提示', {})
                    .then(() => {
                        Session.clear(); // 清除浏览器全部临时缓存
                        window.location.href = '/#/login'; // 去登录页
                    })
                    .catch(() => {
                    });
            }
            return Promise.reject(res);
        } else {
            return res;
        }
    },
    (error) => {
        // 对响应错误做点什么
        const apiError = error.response?.data?.error;
        if (apiError) {
            if (error.response.status === 401 || apiError.code === 'UNAUTHENTICATED') {
                ElMessageBox.alert(`${apiError.message}，请重新登录`, '提示', {})
                    .then(() => {
                        Session.clear();
                        window.location.href = '/#/login';
                    })
                    .catch(() => {});
                return Promise.reject(error.response.data);
            }
            ElMessage.error(apiError.message || '请求失败');
            return Promise.reject(error.response.data);
        }
        if (error.message.indexOf('timeout') != -1) {
            ElMessage.error('网络超时');
        } else if (error.message == 'Network Error') {
            ElMessage.error('网络连接错误');
        } else {
            if (error.response.data) {
                if (error.response.data.msg) {
                    let errCode = error.response.data.code
                    if (errCode && errCode != 0) {
                        // 认证信息异常
                        if (errCode === 401 || errCode === 20401) {
                            ElMessageBox.alert(`${error.response.data.msg}，请重新登录`, '提示', {})
                                .then(() => {
                                    Session.clear(); // 清除浏览器全部临时缓存
                                    window.location.href = '/#/login'; // 去登录页
                                })
                                .catch(() => {
                                });
                            return;
                        }
                    }
                    ElMessage.error(error.response.data.msg)
                } else {
                    ElMessage.error(error.response.statusText)
                }
            } else {
                ElMessage.error(error)
            }
        }
        return Promise.reject(error);
    }
);

// 导出 axios 实例
export default service;
