<template>
  <el-form size="large" class="login-content-form">
    <el-form-item class="login-animation1">
      <el-input text :placeholder="$t('message.account.accountPlaceholder1')" v-model="state.ruleForm.username"
                clearable autocomplete="off">
        <template #prefix>
          <el-icon class="el-input__icon">
            <ele-User/>
          </el-icon>
        </template>
      </el-input>
    </el-form-item>
    <el-form-item class="login-animation2">
      <el-input
          :type="state.isShowPassword ? 'text' : 'password'"
          :placeholder="$t('message.account.accountPlaceholder2')"
          v-model="state.ruleForm.password"
          autocomplete="off"
      >
        <template #prefix>
          <el-icon class="el-input__icon">
            <ele-Unlock/>
          </el-icon>
        </template>
        <template #suffix>
          <i
              class="iconfont el-input__icon login-content-password"
              :class="state.isShowPassword ? 'icon-yincangmima' : 'icon-xianshimima'"
              @click="state.isShowPassword = !state.isShowPassword"
          >
          </i>
        </template>
      </el-input>
    </el-form-item>
    <!--		<el-form-item class="login-animation3">-->
    <!--			<el-col :span="13">-->
    <!--				<el-input-->
    <!--					text-->
    <!--					maxlength="4"-->
    <!--					:placeholder="$t('message.account.accountPlaceholder3')"-->
    <!--					v-model="state.ruleForm.code"-->
    <!--					clearable-->
    <!--					autocomplete="off"-->
    <!--				>-->
    <!--					<template #prefix>-->
    <!--						<el-icon class="el-input__icon"><ele-Position /></el-icon>-->
    <!--					</template>-->
    <!--				</el-input>-->
    <!--			</el-col>-->
    <!--			<el-col :span="1"></el-col>-->
    <!--			<el-col :span="10">-->
    <!--        <el-image class="login-content-code" src="https://www.baidu.com/img/PCtm_d9c8750bed0b3c7d089fa7d55720d6cf.png" v-waves></el-image>-->
    <!--      </el-col>-->
    <!--		</el-form-item>-->
    <el-form-item class="login-animation4">
      <el-button type="primary" class="login-content-submit" round v-waves @click="onSignIn"
                 :loading="state.loading.signIn">
        <span>{{ $t('message.account.accountBtnText') }}</span>
      </el-button>
    </el-form-item>
  </el-form>
</template>

<script setup lang="ts" name="loginAccount">
import {reactive, computed} from 'vue';
import {useRoute, useRouter} from 'vue-router';
import {ElMessage} from 'element-plus';
import {useI18n} from 'vue-i18n';
import {storeToRefs} from 'pinia';
import {useThemeConfig} from '/@/stores/themeConfig';
import {initFrontEndControlRoutes} from '/@/router/frontEnd';
import {Session} from '/@/utils/storage';
import {formatAxis} from '/@/utils/formatTime';
import {encodePassword} from "/@/utils/password";
import {NextLoading} from '/@/utils/loading';
import {useLoginApi} from "/@/api/login";

// 定义变量内容
const {t} = useI18n();
const storesThemeConfig = useThemeConfig();
const {themeConfig} = storeToRefs(storesThemeConfig);
const route = useRoute();
const router = useRouter();
const state = reactive({
  isShowPassword: false,
  ruleForm: {
    username: '',
    password: '',
    // code: '',
  },
  loading: {
    signIn: false,
  },
});

// 时间获取
const currentTime = computed(() => {
  return formatAxis(new Date());
});
// 登录
const onSignIn = async () => {
  state.loading.signIn = true;
  // 发送登录请求
  useLoginApi().signIn({
    ...state.ruleForm,
    password: encodePassword(state.ruleForm.password)
  }).then(async resp => {
    // 存储 token 到浏览器缓存
    Session.set('token', resp.data.token);
    const redirect = typeof route.query?.redirect === 'string' ? route.query.redirect : '';
    // Legacy deep links still need their dynamic menu; the new app does not.
    if (redirect && !redirect.startsWith('/app')) {
      const isNoPower = await initFrontEndControlRoutes();
      if (isNoPower) {
        ElMessage.warning('没有权限');
        Session.clear();
        return;
      }
    }
    signInSuccess(resp.data);
  }).finally(() => {
    state.loading.signIn = false;
  })
};
// 登录成功后的跳转
const signInSuccess = (data: any | undefined) => {
  // 初始化登录成功时间问候语
  let currentTimeInfo = currentTime.value;
  // 登录成功，跳到转首页
  // 如果是复制粘贴的路径，非首页/登录页，那么登录成功后重定向到对应的路径中
  if (route.query?.redirect) {
    router.push({
      path: <string>route.query?.redirect,
      query: Object.keys(<string>route.query?.params).length > 0 ? JSON.parse(<string>route.query?.params) : '',
    });
  } else {
    router.push('/app/home');
  }
  // 登录成功提示
  const signInText = t('message.signInText');
  ElMessage.success(`${currentTimeInfo}，${signInText}`);
  // 添加 loading，防止第一次进入界面时出现短暂空白
  NextLoading.start();
  // }
};
</script>

<style scoped lang="scss">
.login-content-form {
  margin-top: 20px;
  @for $i from 1 through 4 {
    .login-animation#{$i} {
      opacity: 0;
      animation-name: error-num;
      animation-duration: 0.5s;
      animation-fill-mode: forwards;
      animation-delay: calc($i/10) + s;
    }
  }

  .login-content-password {
    display: inline-block;
    width: 20px;
    cursor: pointer;

    &:hover {
      color: #909399;
    }
  }

  .login-content-code {
    width: 100%;
    height: 38px;
    padding: 0;
    font-weight: bold;
    letter-spacing: 5px;
    display: block;
  }

  .login-content-submit {
    width: 100%;
    letter-spacing: 2px;
    font-weight: 300;
    margin-top: 15px;
  }
}
</style>
